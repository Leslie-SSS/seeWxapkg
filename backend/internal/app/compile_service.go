package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/keepbuild/seewxapkg/internal/config"
	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
	"github.com/keepbuild/seewxapkg/internal/infra/events"
	"github.com/keepbuild/seewxapkg/internal/infra/process"
	"github.com/keepbuild/seewxapkg/internal/infra/queue"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
	"github.com/keepbuild/seewxapkg/internal/pipeline/classifier"
	dec "github.com/keepbuild/seewxapkg/internal/pipeline/decrypt"
	"github.com/keepbuild/seewxapkg/internal/pipeline/normalize"
	recovery "github.com/keepbuild/seewxapkg/internal/pipeline/recover"
	"github.com/keepbuild/seewxapkg/internal/pipeline/verify"
	"github.com/keepbuild/seewxapkg/internal/report"
	legacyservice "github.com/keepbuild/seewxapkg/internal/service"
)

type StartCompileCommand struct {
	AppID           string
	Beautify        bool
	Decompile       bool
	RemoveGuideHTML bool
	File            *multipart.FileHeader
}

type CompileService struct {
	cfg        *config.Config
	repo       task.Repository
	broker     *events.Broker
	queue      queue.JobQueue
	nodeRunner *process.NodeRunner
}

func NewCompileService(cfg *config.Config, repo task.Repository, broker *events.Broker, jobQueue queue.JobQueue) *CompileService {
	return &CompileService{
		cfg:    cfg,
		repo:   repo,
		broker: broker,
		queue:  jobQueue,
		nodeRunner: &process.NodeRunner{
			Binary:   cfg.NodeBinary,
			Timeout:  time.Duration(cfg.NodeExecTimeoutSeconds) * time.Second,
			MemoryMB: cfg.NodeExecMemoryMB,
		},
	}
}

// Readiness verifies the local capabilities required to accept a task. Queue
// and repository backends still enforce their own errors when a task is
// created; this check focuses on writable artifact storage and the optional
// formatter process, which previously failed silently.
func (s *CompileService) Readiness() (map[string]interface{}, error) {
	capabilities := map[string]interface{}{
		"repository": s.cfg.TaskRepoDriver,
		"queue":      s.cfg.QueueDriver,
		"beautify":   legacyservice.BeautifyStatus(),
		"recovery": map[string]bool{
			"native":   s.cfg.NativeRecoverEnabled,
			"fallback": s.cfg.FallbackRecoverEnabled,
			"verify":   s.cfg.VerificationEnabled,
		},
	}
	for label, dir := range map[string]string{"temp": s.cfg.TempDir, "output": s.cfg.OutputDir} {
		probe, err := os.CreateTemp(dir, ".readiness-*")
		if err != nil {
			return capabilities, fmt.Errorf("%s directory is not writable: %w", label, err)
		}
		name := probe.Name()
		if err := probe.Close(); err != nil {
			_ = os.Remove(name)
			return capabilities, fmt.Errorf("close %s readiness probe: %w", label, err)
		}
		if err := os.Remove(name); err != nil {
			return capabilities, fmt.Errorf("remove %s readiness probe: %w", label, err)
		}
	}
	if s.cfg.BeautifyEnabled {
		stats := legacyservice.BeautifyStatus()
		enabled, _ := stats["enabled"].(bool)
		healthy, _ := stats["healthy"].(bool)
		if !enabled || !healthy {
			return capabilities, fmt.Errorf("safe formatter is configured but unavailable")
		}
	}
	if s.cfg.FallbackRecoverEnabled || s.cfg.VerificationEnabled {
		binary := s.cfg.NodeBinary
		if binary == "" {
			binary = "node"
		}
		if _, err := exec.LookPath(binary); err != nil {
			return capabilities, fmt.Errorf("node.js runtime required by recovery is unavailable: %w", err)
		}
	}
	resources := map[string]bool{}
	if s.cfg.FallbackRecoverEnabled {
		_, err := process.ResolveExistingPath(
			filepath.Join("backend", "internal", "beautify", "wxappUnpacker", "wuWxapkg.js"),
			filepath.Join("internal", "beautify", "wxappUnpacker", "wuWxapkg.js"),
		)
		if err != nil {
			return capabilities, fmt.Errorf("fallback recovery resource unavailable: %w", err)
		}
		resources["fallbackScript"] = true
	}
	if s.cfg.VerificationEnabled {
		_, err := process.ResolveExistingPath(
			filepath.Join("backend", "internal", "beautify", "runtime", "verify_artifacts.js"),
			filepath.Join("internal", "beautify", "runtime", "verify_artifacts.js"),
		)
		if err != nil {
			return capabilities, fmt.Errorf("artifact verifier resource unavailable: %w", err)
		}
		resources["artifactVerifier"] = true
	}
	capabilities["resources"] = resources
	return capabilities, nil
}

func (s *CompileService) StartTask(ctx context.Context, cmd StartCompileCommand) (*task.Task, error) {
	createdAt := time.Now()
	t := &task.Task{
		ID:     uuid.New().String(),
		Status: task.TaskQueued,
		RequestedOptions: task.RequestedOptions{
			Beautify:        cmd.Beautify,
			Decompile:       cmd.Decompile,
			RemoveGuideHTML: cmd.RemoveGuideHTML,
		},
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}

	dirs, err := storage.EnsureTaskDirs(s.cfg.TempDir, t.ID)
	if err != nil {
		return nil, err
	}
	if _, err := storage.SaveUploadedFile(dirs, cmd.File); err != nil {
		return nil, err
	}
	keepSecret := false
	defer func() {
		if !keepSecret {
			_ = storage.DeleteAppIDSecret(dirs)
			_ = storage.DeleteTaskInput(dirs)
		}
	}()
	if err := storage.SaveAppIDSecret(dirs, cmd.AppID); err != nil {
		return nil, err
	}

	s.broker.Create(t.ID)
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}
	s.publish(t, task.TaskEvent{
		Type:    "progress",
		Stage:   "queued",
		Status:  string(task.TaskQueued),
		Percent: 0,
		Message: "任务已创建，等待处理",
	})

	if s.queue != nil {
		if err := s.queue.Enqueue(ctx, t.ID); err != nil {
			return nil, s.markFailed(ctx, t, "queue_enqueue_failed", "任务暂时无法进入处理队列", err)
		}
	} else {
		go func() {
			if err := s.RunTask(context.Background(), t.ID); err != nil {
				log.Printf("[Task] pipeline failed: %v", err)
			}
		}()
	}
	keepSecret = true

	return t.Clone(), nil
}

func (s *CompileService) RunTask(ctx context.Context, taskID string) (runErr error) {
	var t *task.Task
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr := fmt.Errorf("pipeline panic: %v", recovered)
			log.Printf("[Task] recovered pipeline panic: %v", recovered)
			if t == nil {
				runErr = panicErr
				return
			}

			// Persist a truthful terminal state even when the request/worker context
			// is already cancelled. Keep this recovery bounded so shutdown cannot hang.
			finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			var finalizeErr error
			func() {
				defer func() {
					if nested := recover(); nested != nil {
						finalizeErr = fmt.Errorf("panic while finalizing recovered panic: %v", nested)
					}
				}()
				finalizeErr = s.markFailed(finalizeCtx, t, "internal_panic", "任务处理发生内部异常，已安全终止", panicErr)
			}()
			if finalizeErr != nil && !errors.Is(finalizeErr, panicErr) {
				runErr = fmt.Errorf("%w; persist terminal state: %v", panicErr, finalizeErr)
				return
			}
			runErr = panicErr
		}
	}()

	var err error
	t, err = s.repo.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if t.Status == task.TaskCompleted || t.Status == task.TaskPartial || t.Status == task.TaskFailed {
		dirs, dirsErr := storage.EnsureTaskDirs(s.cfg.TempDir, taskID)
		if dirsErr != nil {
			return dirsErr
		}
		return errors.Join(storage.DeleteAppIDSecret(dirs), storage.DeleteTaskInput(dirs))
	}

	dirs, err := storage.EnsureTaskDirs(s.cfg.TempDir, taskID)
	if err != nil {
		return s.markFailed(ctx, t, "task_dirs_failed", "创建任务目录失败", err)
	}
	if t.Status != task.TaskQueued {
		if err := storage.ResetTaskWorkspace(dirs); err != nil {
			return s.markFailed(ctx, t, "retry_workspace_failed", "清理上次未完成的处理结果失败", err)
		}
		if err := removeIfExists(filepath.Join(s.cfg.OutputDir, t.ID+".zip")); err != nil {
			return s.markFailed(ctx, t, "retry_archive_failed", "清理上次未完成的下载文件失败", err)
		}
		resetTaskForRetry(t)
		if err := s.repo.Update(ctx, t); err != nil {
			return err
		}
	}

	inputPath := storage.InputFilePath(dirs)
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return s.markFailed(ctx, t, "input_read_failed", "读取上传文件失败", err)
	}

	profile, err := s.classify(ctx, t, data, "")
	if err != nil {
		return s.markFailed(ctx, t, "classify_failed", "包类型识别失败", err)
	}
	inputWasEncrypted := profile.IsEncrypted
	t.PackageProfile = profile
	if err := s.repo.Update(ctx, t); err != nil {
		return err
	}
	appID, err := storage.ReadAppIDSecret(dirs)
	if err != nil {
		return s.markFailed(ctx, t, "app_id_read_failed", "读取解密凭据失败", err)
	}
	decryptedData, decryptErr := s.decrypt(ctx, t, data, appID)
	// With sample collection enabled, keep the package (decrypted bytes when
	// available) and the one-shot AppID for offline analysis before the
	// credential is destroyed. Best-effort: a storage failure must not change
	// task outcomes.
	if sampleErr := s.saveDiagnosticSample(t, dirs, data, decryptedData, appID); sampleErr != nil {
		log.Printf("[Task] save diagnostic sample failed (%T)", sampleErr)
	}
	deleteSecretErr := storage.DeleteAppIDSecret(dirs)
	if deleteSecretErr != nil {
		return s.markFailed(ctx, t, "app_id_cleanup_failed", "清理解密凭据失败，任务已安全终止", deleteSecretErr)
	}
	if decryptErr != nil {
		if errors.Is(decryptErr, dec.ErrNeedAppID) {
			return s.markFailed(ctx, t, "app_id_required", "这是加密包，需要提供正确的小程序 AppID 才能解密", decryptErr)
		}
		if errors.Is(decryptErr, dec.ErrBadAppID) {
			return s.markFailed(ctx, t, "app_id_invalid", "AppID 格式错误，应为 wx 开头的 18 位标识", decryptErr)
		}
		return s.markFailed(ctx, t, "decrypt_failed", "解密失败", decryptErr)
	}

	_, err = s.unpack(ctx, t, decryptedData, dirs.SourceDir)
	if err != nil {
		return s.markFailed(ctx, t, "unpack_failed", "解包失败", err)
	}
	// The user chose to drop WeChat 4.x page-entry `.html` runtime-guide
	// scaffolds; remove them before any later stage (collect/format/verify)
	// can touch them, so the delivered tree and ZIP stay source-only.
	if t.RequestedOptions.RemoveGuideHTML {
		if err := storage.RemoveGuideHTMLFiles(dirs.SourceDir); err != nil {
			return s.markFailed(ctx, t, "guide_files_cleanup_failed", "清理运行时引导文件失败", err)
		}
	}

	profile, err = s.classify(ctx, t, decryptedData, dirs.SourceDir)
	if err != nil {
		return s.markFailed(ctx, t, "classify_failed", "解包后包类型识别失败", err)
	}
	// The second classification sees decrypted bytes. Preserve how the user
	// supplied the package while keeping all structural signals from the
	// decrypted/extracted representation.
	profile.IsEncrypted = inputWasEncrypted
	t.PackageProfile = profile

	normalized, err := s.normalize(ctx, t, dirs.SourceDir, profile)
	if err != nil {
		return s.markFailed(ctx, t, "normalize_failed", "规范化包结构失败", err)
	}

	manifestResult, err := s.recoverManifest(ctx, t, normalized, dirs.SourceDir, dirs.ReportsDir)
	if err != nil {
		return s.markFailed(ctx, t, "manifest_recover_failed", "manifest 恢复失败", err)
	}
	artifactFiles := []task.ArtifactFile{
		{
			Path:   filepath.ToSlash(filepath.Join("src", filepath.Base(manifestResult.OutputPath))),
			Kind:   "json",
			Source: "manifest",
		},
	}

	decompileArtifacts, fallbackUsed, decompilePartial, err := s.recoverDecompile(ctx, t, normalized, decryptedData, dirs)
	if err != nil {
		return s.markFailed(ctx, t, "decompile_failed", "深度恢复阶段失败", err)
	}
	artifactFiles = append(artifactFiles, decompileArtifacts...)

	if t.RequestedOptions.Beautify {
		s.beginStage(ctx, t, task.TaskFormatting, 84, "正在执行语义安全的最终格式化...")
		formatResult, err := legacyservice.FormatSourceTree(dirs.SourceDir)
		if err != nil {
			return s.markFailed(ctx, t, "format_failed", "最终格式化阶段失败", err)
		}
		if err := storage.WriteJSON(filepath.Join(dirs.ReportsDir, "format-report.json"), formatResult); err != nil {
			return s.markFailed(ctx, t, "format_report_failed", "写入格式化报告失败", err)
		}
		formatDiagnostics := make([]pkg.Diagnostic, 0)
		if formatResult.Failed > 0 {
			formatDiagnostics = append(formatDiagnostics, pkg.Warn("format.files.failed", fmt.Sprintf("%d 个文件格式化失败，已保留原始内容", formatResult.Failed), "formatting", dirs.SourceDir))
		}
		if formatResult.Skipped > 0 {
			formatDiagnostics = append(formatDiagnostics, pkg.Warn("format.files.skipped", fmt.Sprintf("%d 个文件未格式化，已保留原始内容", formatResult.Skipped), "formatting", dirs.SourceDir))
		}
		// Surface up to a few per-file warnings so users can locate the files
		// that kept their original content; the full list stays in
		// format-report.json. Truncate the warning text to bound state size.
		const maxWarningSamples = 10
		warningSamples := 0
		for _, fileResult := range formatResult.Files {
			if warningSamples >= maxWarningSamples {
				break
			}
			if fileResult.Warning == "" {
				continue
			}
			warningSamples++
			warningText := fileResult.Warning
			if runes := []rune(warningText); len(runes) > 120 {
				warningText = string(runes[:120])
			}
			formatDiagnostics = append(formatDiagnostics, pkg.Warn("format.files.warning", warningText, "formatting", fileResult.Path))
		}
		s.finishStage(ctx, t, string(task.TaskFormatting), formatResult.Success, formatResult.Partial, "最终格式化阶段完成", map[string]interface{}{
			"engine":    "safe-format",
			"formatted": formatResult.Formatted,
			"unchanged": formatResult.Unchanged,
			"skipped":   formatResult.Skipped,
			"failed":    formatResult.Failed,
			"report":    "format-report.json",
		}, formatDiagnostics)
		decompilePartial = decompilePartial || formatResult.Partial
	}

	manifestVerifyResult, artifactVerifyResult, err := s.verify(ctx, t, normalized, dirs.SourceDir)
	if err != nil {
		return s.markFailed(ctx, t, "verify_failed", "恢复结果验证失败", err)
	}

	t.RecoveryScore = verify.ComputeRecoveryScore(manifestVerifyResult, artifactVerifyResult, t.RequestedOptions.Decompile, fallbackUsed)
	t.Diagnostics = dedupeDiagnostics(t.Diagnostics)

	reportPath := filepath.Join(dirs.ReportsDir, "recovery-report.json")
	diagnosticsPath := filepath.Join(dirs.ReportsDir, "diagnostics.json")

	t.ArtifactSummary = s.buildArtifactSummary(t, dirs, reportPath, diagnosticsPath, artifactFiles)
	finalFileCount := t.ArtifactSummary.FileCount
	status, code, message := determineFinalStatus(manifestVerifyResult, artifactVerifyResult, decompilePartial)
	switch status {
	case task.TaskPartial:
		if t.PackageProfile != nil && t.PackageProfile.IsGamePackage {
			// Mini-game packages render via Canvas and carry no WXML pages;
			// the generic "missing WXML" message would mislead.
			message = fmt.Sprintf("已整理 %d 个源码文件（小游戏包，无 WXML 页面，以 Canvas 渲染）", finalFileCount)
			break
		}
		message = fmt.Sprintf("已整理 %d 个源码文件；%s，详见 recovery-report.json", finalFileCount, message)
	case task.TaskCompleted:
		message = fmt.Sprintf("处理完成，共整理 %d 个源码文件", finalFileCount)
	}

	if err := s.packageResult(ctx, t, dirs); err != nil {
		return s.markFailed(ctx, t, "package_failed", "打包结果失败", err)
	}
	s.refreshArtifactSummary(t)

	return s.finalizeTask(ctx, t, status, code, message, nil)
}

func (s *CompileService) classify(ctx context.Context, t *task.Task, data []byte, extractedDir string) (*pkg.PackageProfile, error) {
	s.beginStage(ctx, t, task.TaskClassifying, 5, "正在识别包类型与版本特征...")
	profile, err := classifier.DetectPackageProfile(data, extractedDir)
	if err != nil {
		return nil, err
	}

	stageDiagnostics := []pkg.Diagnostic{
		pkg.Info("classifier.variant", "已识别包变体: "+profile.SuspectedVariant, "classifying", extractedDir),
	}
	s.finishStage(ctx, t, string(task.TaskClassifying), true, false, "包类型识别完成", map[string]interface{}{
		"variant":          profile.SuspectedVariant,
		"isEncrypted":      profile.IsEncrypted,
		"indexFileCount":   profile.IndexFileCount,
		"supportsRecovery": profile.SupportsNativeRecovery(),
	}, stageDiagnostics)
	return profile, nil
}

func (s *CompileService) decrypt(ctx context.Context, t *task.Task, data []byte, appID string) ([]byte, error) {
	s.beginStage(ctx, t, task.TaskDecrypting, 15, "正在解密或校验 wxapkg 数据...")
	decrypted, err := dec.DecryptWxapkg(data, appID)
	if err != nil {
		return nil, err
	}

	mode := dec.DetectEncryptionMode(data)
	message := "检测到未加密包，直接进入解包"
	if mode == dec.EncryptionEncrypted {
		message = "解密完成"
	}
	s.finishStage(ctx, t, string(task.TaskDecrypting), true, false, message, map[string]interface{}{
		"mode": string(mode),
	}, nil)
	return decrypted, nil
}

func (s *CompileService) unpack(ctx context.Context, t *task.Task, data []byte, outputDir string) (*legacyservice.UnpackResult, error) {
	s.beginStage(ctx, t, task.TaskUnpacking, 32, "正在解包 wxapkg...")
	// Extraction must remain byte-for-byte faithful. Formatting is a final,
	// explicitly reported stage after all recovery engines have completed.
	result, err := legacyservice.UnpackWxapkg(data, outputDir, false)
	if err != nil {
		return nil, err
	}

	s.finishStage(ctx, t, string(task.TaskUnpacking), true, false, "基础解包完成", map[string]interface{}{
		"fileCount": result.FileCount,
	}, nil)
	return result, nil
}

func (s *CompileService) normalize(ctx context.Context, t *task.Task, extractedDir string, profile *pkg.PackageProfile) (*pkg.NormalizedPackage, error) {
	s.beginStage(ctx, t, task.TaskNormalizing, 48, "正在将包结构统一转换为中间表示...")
	normalized, err := normalize.NormalizePackage(extractedDir, profile)
	if err != nil {
		return nil, err
	}

	s.finishStage(ctx, t, string(task.TaskNormalizing), true, false, "结构规范化完成", map[string]interface{}{
		"pages":       len(normalized.Manifest.Pages),
		"scripts":     len(normalized.Scripts),
		"styles":      len(normalized.Styles),
		"templates":   len(normalized.Templates),
		"diagnostics": len(normalized.Diagnostics),
	}, normalized.Diagnostics)
	return normalized, nil
}

func (s *CompileService) recoverManifest(ctx context.Context, t *task.Task, normalized *pkg.NormalizedPackage, sourceDir, reportsDir string) (*recovery.ManifestRecoveryResult, error) {
	s.beginStage(ctx, t, task.TaskRecoveringManifest, 58, "正在恢复规范化 manifest...")
	result, err := recovery.RecoverManifest(normalized, sourceDir, reportsDir)
	if err != nil {
		return nil, err
	}
	s.finishStage(ctx, t, string(task.TaskRecoveringManifest), result.Success, false, "manifest 恢复完成", map[string]interface{}{
		"pageCount": result.PageCount,
		"sources":   result.Sources,
	}, result.Diagnostics)
	return result, nil
}

func (s *CompileService) recoverDecompile(ctx context.Context, t *task.Task, normalized *pkg.NormalizedPackage, decryptedData []byte, dirs storage.TaskDirs) ([]task.ArtifactFile, bool, bool, error) {
	if !t.RequestedOptions.Decompile {
		return nil, false, false, nil
	}

	var artifactFiles []task.ArtifactFile
	fallbackUsed := false
	decompilePartial := false
	needsFallback := false
	usedInferredOutput := false

	s.beginStage(ctx, t, task.TaskRecoveringJS, 66, "正在执行深度恢复引擎...")
	if s.cfg.NativeRecoverEnabled {
		jsResult, err := recovery.RecoverJS(normalized, dirs.SourceDir, dirs.ReportsDir)
		if err != nil {
			return nil, false, false, err
		}
		artifactFiles = append(artifactFiles, toArtifactFiles(jsResult.Files)...)
		s.finishStage(ctx, t, string(task.TaskRecoveringJS), jsResult.Success, jsResult.Partial, chooseRecoveryMessage("JS", jsResult.Success), map[string]interface{}{
			"recovered":       jsResult.Recovered,
			"generated":       jsResult.Generated,
			"native":          jsResult.Native,
			"report":          filepath.Base(jsResult.ReportPath),
			"engine":          "native",
			"sourceBreakdown": countRecoveredSources(jsResult.Files),
		}, jsResult.Diagnostics)
		needsFallback = needsFallback || jsResult.Partial || !jsResult.Success
		usedInferredOutput = usedInferredOutput || jsResult.Generated > 0
	} else {
		needsFallback = true
		s.finishStage(ctx, t, string(task.TaskRecoveringJS), false, true, "原生 JS 恢复已禁用", nil, []pkg.Diagnostic{
			pkg.Warn("recover.js.disabled", "原生 JS 恢复被配置禁用", "recovering_js", dirs.SourceDir),
		})
	}

	s.beginStage(ctx, t, task.TaskRecoveringWXML, 72, "正在校准 WXML 恢复结果...")
	if s.cfg.NativeRecoverEnabled {
		wxmlResult, err := recovery.RecoverWXML(normalized, dirs.SourceDir, dirs.ReportsDir)
		if err != nil {
			return nil, false, false, err
		}
		artifactFiles = append(artifactFiles, toArtifactFiles(wxmlResult.Files)...)
		s.finishStage(ctx, t, string(task.TaskRecoveringWXML), wxmlResult.Success, wxmlResult.Partial, chooseRecoveryMessage("WXML", wxmlResult.Success), map[string]interface{}{
			"recovered":       wxmlResult.Recovered,
			"generated":       wxmlResult.Generated,
			"native":          wxmlResult.Native,
			"report":          filepath.Base(wxmlResult.ReportPath),
			"engine":          "native",
			"sourceBreakdown": countRecoveredSources(wxmlResult.Files),
		}, wxmlResult.Diagnostics)
		needsFallback = needsFallback || wxmlResult.Partial || !wxmlResult.Success
		usedInferredOutput = usedInferredOutput || wxmlResult.Generated > 0
	} else {
		needsFallback = true
		s.finishStage(ctx, t, string(task.TaskRecoveringWXML), false, true, "原生 WXML 恢复已禁用", nil, []pkg.Diagnostic{
			pkg.Warn("recover.wxml.disabled", "原生 WXML 恢复被配置禁用", "recovering_wxml", dirs.SourceDir),
		})
	}

	s.beginStage(ctx, t, task.TaskRecoveringWXSS, 78, "正在校准 WXSS 恢复结果...")
	if s.cfg.NativeRecoverEnabled {
		wxssResult, err := recovery.RecoverWXSS(normalized, dirs.SourceDir, dirs.ReportsDir)
		if err != nil {
			return nil, false, false, err
		}
		artifactFiles = append(artifactFiles, toArtifactFiles(wxssResult.Files)...)
		s.finishStage(ctx, t, string(task.TaskRecoveringWXSS), wxssResult.Success, wxssResult.Partial, chooseRecoveryMessage("WXSS", wxssResult.Success), map[string]interface{}{
			"recovered":       wxssResult.Recovered,
			"generated":       wxssResult.Generated,
			"native":          wxssResult.Native,
			"report":          filepath.Base(wxssResult.ReportPath),
			"engine":          "native",
			"sourceBreakdown": countRecoveredSources(wxssResult.Files),
		}, wxssResult.Diagnostics)
		needsFallback = needsFallback || wxssResult.Partial || !wxssResult.Success
		usedInferredOutput = usedInferredOutput || wxssResult.Generated > 0
	} else {
		needsFallback = true
		s.finishStage(ctx, t, string(task.TaskRecoveringWXSS), false, true, "原生 WXSS 恢复已禁用", nil, []pkg.Diagnostic{
			pkg.Warn("recover.wxss.disabled", "原生 WXSS 恢复被配置禁用", "recovering_wxss", dirs.SourceDir),
		})
	}

	if needsFallback && normalized.Profile.IsSubPackage {
		decompilePartial = true
		s.beginStage(ctx, t, task.TaskFallbackRecovering, 82, "检测到独立分包，正在保留可验证的基础产物...")
		diagnostics := []pkg.Diagnostic{
			pkg.Warn("recover.fallback.subpackage_requires_main", "独立分包缺少主包运行时，已跳过不可靠的 fallback；请与主包一并恢复", "fallback_recovering", dirs.SourceDir),
		}
		s.finishStage(ctx, t, string(task.TaskFallbackRecovering), false, true, "独立分包无法在缺少主包时可靠拆分", map[string]interface{}{
			"used":   false,
			"engine": "subpackage-guard",
		}, diagnostics)
	} else if needsFallback && s.cfg.FallbackRecoverEnabled {
		s.beginStage(ctx, t, task.TaskFallbackRecovering, 82, "主轨恢复不完整，正在执行 fallback 引擎...")
		fallbackDir := filepath.Join(dirs.RootDir, "fallback")
		if err := os.MkdirAll(fallbackDir, 0700); err != nil {
			return nil, false, false, err
		}
		inputCopy := filepath.Join(fallbackDir, "input.wxapkg")
		if err := os.WriteFile(inputCopy, decryptedData, 0600); err != nil {
			return nil, false, false, err
		}
		defer func() { _ = os.RemoveAll(fallbackDir) }()
		absScript, err := process.ResolveExistingPath(
			filepath.Join("backend", "internal", "beautify", "wxappUnpacker", "wuWxapkg.js"),
			filepath.Join("internal", "beautify", "wxappUnpacker", "wuWxapkg.js"),
		)
		if err != nil {
			return nil, false, false, err
		}
		fallbackResult, err := recovery.RunFallbackRecovery(ctx, s.nodeRunner, absScript, inputCopy, fallbackDir)
		removeInputErr := os.Remove(inputCopy)
		if err != nil {
			return nil, false, false, err
		}
		if removeInputErr != nil && !os.IsNotExist(removeInputErr) {
			return nil, false, false, fmt.Errorf("remove fallback input: %w", removeInputErr)
		}
		fallbackUsed = fallbackResult.Success
		decompilePartial = decompilePartial || fallbackResult.Partial || !fallbackResult.Success
		if fallbackResult.Success {
			if err := recovery.MergeFallbackArtifacts(dirs.SourceDir, fallbackResult.OutputDir); err != nil {
				return nil, false, false, err
			}
			artifactFiles = append(artifactFiles, toArtifactFiles(fallbackResult.Files)...)
		}
		s.finishStage(ctx, t, string(task.TaskFallbackRecovering), fallbackResult.Success && !fallbackResult.Partial, fallbackResult.Partial || !fallbackResult.Success, "fallback 恢复阶段结束", map[string]interface{}{
			"used":            fallbackResult.Success,
			"fallback":        "wxappUnpacker",
			"engine":          "fallback",
			"status":          fallbackResult.Status,
			"partial":         fallbackResult.Partial,
			"sourceBreakdown": countRecoveredSources(fallbackResult.Files),
		}, fallbackResult.Diagnostics)
		if err := os.RemoveAll(fallbackDir); err != nil {
			return nil, false, false, fmt.Errorf("remove fallback workspace: %w", err)
		}
	} else if needsFallback {
		decompilePartial = true
	}

	jsReady := hasExtFiles(dirs.SourceDir, ".js")
	wxmlReady := hasExtFiles(dirs.SourceDir, ".wxml")
	decompilePartial = decompilePartial || usedInferredOutput || !(jsReady && wxmlReady)
	return dedupeArtifactFiles(artifactFiles), fallbackUsed, decompilePartial, nil
}

func (s *CompileService) verify(ctx context.Context, t *task.Task, normalized *pkg.NormalizedPackage, sourceDir string) (*verify.ManifestVerifyResult, *verify.ArtifactVerifyResult, error) {
	s.beginStage(ctx, t, task.TaskVerifying, 86, "正在验证恢复结果完整性...")
	if !s.cfg.VerificationEnabled {
		manifestResult := &verify.ManifestVerifyResult{
			Success:   true,
			PageCount: len(normalized.Manifest.Pages),
			Diagnostics: []pkg.Diagnostic{
				pkg.Warn("verify.disabled", "验证阶段已禁用，任务将按 partial 语义交付", "verifying", sourceDir),
			},
		}
		artifactResult := &verify.ArtifactVerifyResult{
			Success:         false,
			TotalPages:      len(normalized.Manifest.Pages),
			VerifierPassed:  false,
			CriticalFailure: false,
			Diagnostics: []pkg.Diagnostic{
				pkg.Warn("verify.disabled", "验证阶段已禁用，跳过 parser 级校验", "verifying", sourceDir),
			},
		}
		diagnostics := append([]pkg.Diagnostic(nil), manifestResult.Diagnostics...)
		diagnostics = append(diagnostics, artifactResult.Diagnostics...)
		s.finishStage(ctx, t, string(task.TaskVerifying), false, true, "验证阶段已跳过", map[string]interface{}{
			"engine": "disabled",
		}, diagnostics)
		return manifestResult, artifactResult, nil
	}
	manifestResult, err := verify.VerifyManifest(normalized, sourceDir)
	if err != nil {
		return nil, nil, err
	}
	artifactResult, err := verify.VerifyArtifacts(s.nodeRunner, normalized, sourceDir)
	if err != nil {
		return nil, nil, err
	}

	diagnostics := append([]pkg.Diagnostic(nil), manifestResult.Diagnostics...)
	diagnostics = append(diagnostics, artifactResult.Diagnostics...)
	verificationPassed := manifestResult.Success && artifactResult.Success
	s.finishStage(ctx, t, string(task.TaskVerifying), verificationPassed, !verificationPassed, "恢复结果验证完成", map[string]interface{}{
		"missingPages":                len(manifestResult.MissingPages),
		"invalidTabBarPages":          len(manifestResult.InvalidTabBarPages),
		"pageTriplets":                artifactResult.PageTriplets,
		"totalPages":                  artifactResult.TotalPages,
		"manifestPassed":              manifestResult.Success,
		"parserPassed":                artifactResult.ParserPassed,
		"wxmlQualityPassed":           artifactResult.WXMLQualityPassed,
		"wxmlQualityIssueFiles":       artifactResult.WXMLQualityIssueFiles,
		"wxmlPlaceholderCount":        artifactResult.WXMLPlaceholderCount,
		"wxmlUnresolvedMarkerFiles":   artifactResult.WXMLUnresolvedMarkerFiles,
		"wxmlUnresolvedMarkers":       artifactResult.WXMLUnresolvedMarkers,
		"wxmlUnresolvedTextMarkers":   artifactResult.WXMLUnresolvedTextMarkers,
		"wxmlUnresolvedAttrMarkers":   artifactResult.WXMLUnresolvedAttributeMarkers,
		"wxmlSuspiciousEventBindings": artifactResult.WXMLSuspiciousEventBindings,
		"wxmlDynamicEventBindings":    artifactResult.WXMLDynamicEventBindings,
		"artifactPassed":              artifactResult.Success,
		"engine":                      "static-verifier",
	}, diagnostics)
	return manifestResult, artifactResult, nil
}

func (s *CompileService) packageResult(ctx context.Context, t *task.Task, dirs storage.TaskDirs) error {
	s.beginStage(ctx, t, task.TaskPackaging, 94, "正在打包恢复结果...")
	zipPath := filepath.Join(s.cfg.OutputDir, t.ID+".zip")
	archiveEntries, err := storage.ZipDirWithPrefixEntries(dirs.SourceDir, zipPath, "src")
	if err != nil {
		return err
	}
	zipManifest, err := report.BuildZipManifest(t.ID, archiveEntries, "src")
	if err != nil {
		return err
	}
	if err := report.WriteZipManifest(filepath.Join(dirs.ReportsDir, "zip-manifest.json"), zipManifest); err != nil {
		return err
	}
	archiveInfo, err := os.Stat(zipPath)
	if err != nil {
		return err
	}

	s.finishStage(ctx, t, string(task.TaskPackaging), true, false, "结果已打包", map[string]interface{}{
		"archiveFile":   filepath.Base(zipPath),
		"archiveSize":   archiveInfo.Size(),
		"downloadReady": true,
		"zipManifest":   "report?name=zip-manifest",
		"archiveRoot":   "src/",
	}, nil)
	return nil
}

func (s *CompileService) buildArtifactSummary(t *task.Task, dirs storage.TaskDirs, reportPath, diagnosticsPath string, artifactFiles []task.ArtifactFile) *task.ArtifactSummary {
	files := collectArtifactFiles(dirs.SourceDir, artifactFiles)
	return &task.ArtifactSummary{
		FileCount:       len(files),
		DownloadURL:     "/api/download/" + t.ID,
		ReportURL:       "/api/tasks/" + t.ID + "/report",
		DiagnosticsURL:  "/api/tasks/" + t.ID + "/diagnostics",
		ArtifactsURL:    "/api/tasks/" + t.ID + "/artifacts",
		PackageProfile:  "/api/tasks/" + t.ID + "/report?name=package-profile",
		Files:           files,
		ReportPath:      reportPath,
		DiagnosticsPath: diagnosticsPath,
		ArtifactsPath:   filepath.Join(dirs.ReportsDir, "artifacts.json"),
		SourceBreakdown: countArtifactSources(files),
	}
}

func (s *CompileService) markFailed(ctx context.Context, t *task.Task, code, msg string, cause error) error {
	return s.finalizeTask(ctx, t, task.TaskFailed, code, msg, cause)
}

// saveDiagnosticSample retains the package (decrypted bytes when available)
// and the one-shot AppID for offline analysis. Only invoked before the
// credential is destroyed; successful tasks delete their sample at finalize.
func (s *CompileService) saveDiagnosticSample(t *task.Task, dirs storage.TaskDirs, original, decrypted []byte, appID string) error {
	if s.cfg.DiagnosticSamplesDir == "" {
		return nil
	}
	data := original
	if decrypted != nil {
		data = decrypted
	}
	if err := storage.SaveDiagnosticSample(s.cfg.DiagnosticSamplesDir, t.ID, data, appID); err != nil {
		return err
	}
	return nil
}

// discardDiagnosticSample removes a collected sample for a task that ended up
// completing: only failed/partial inputs are worth keeping for analysis.
func (s *CompileService) discardDiagnosticSample(t *task.Task) {
	if s.cfg.DiagnosticSamplesDir == "" {
		return
	}
	if err := os.RemoveAll(filepath.Join(s.cfg.DiagnosticSamplesDir, t.ID)); err != nil {
		log.Printf("[Task] discard diagnostic sample failed (%T)", err)
	}
}

func (s *CompileService) finalizeTask(ctx context.Context, t *task.Task, status task.TaskStatus, code, msg string, cause error) error {
	msg = report.SanitizeText(msg)
	now := time.Now()
	dirs, dirsErr := storage.EnsureTaskDirs(s.cfg.TempDir, t.ID)
	if dirsErr == nil {
		dirsErr = storage.DeleteAppIDSecret(dirs)
	}
	if dirsErr != nil {
		status = task.TaskFailed
		code = "app_id_cleanup_failed"
		msg = "清理解密凭据失败，任务已安全终止"
		cause = errors.Join(cause, dirsErr)
	}
	applyTerminalState(t, status, code, msg, cause, now)
	if status == task.TaskCompleted {
		// Sample collection is for failed/partial inputs only.
		s.discardDiagnosticSample(t)
	}
	if err := s.syncTaskReports(t); err != nil {
		return err
	}
	if err := s.repo.Update(ctx, t); err != nil {
		return err
	}
	if dirsErr == nil {
		if inputCleanupErr := storage.DeleteTaskInput(dirs); inputCleanupErr != nil {
			status = task.TaskFailed
			code = "input_cleanup_failed"
			msg = "清理原始上传文件失败，任务已安全终止"
			cause = errors.Join(cause, inputCleanupErr)
			applyTerminalState(t, status, code, msg, cause, time.Now())
			if err := s.syncTaskReports(t); err != nil {
				return err
			}
			if err := s.repo.Update(ctx, t); err != nil {
				return err
			}
		}
	}

	eventType := "complete"
	if status == task.TaskPartial {
		eventType = "partial"
	}
	if status == task.TaskFailed {
		eventType = "error"
	}

	event := task.TaskEvent{
		Type:             eventType,
		Stage:            string(status),
		Status:           string(status),
		Percent:          100,
		Message:          msg,
		TaskID:           t.ID,
		DiagnosticsCount: len(t.Diagnostics),
	}
	if t.ArtifactSummary != nil {
		event.FileCount = t.ArtifactSummary.FileCount
		event.DownloadURL = t.ArtifactSummary.DownloadURL
		event.ReportURL = t.ArtifactSummary.ReportURL
		event.DiagnosticsURL = t.ArtifactSummary.DiagnosticsURL
	}
	if status == task.TaskFailed {
		if code != "" {
			event.ErrorCode = code
		}
		if t.ErrorMessage != nil {
			event.Error = *t.ErrorMessage
		}
	}
	s.publish(t, event)
	if status == task.TaskFailed {
		if cause != nil {
			return cause
		}
		return errors.New(msg)
	}
	return nil
}

func applyTerminalState(t *task.Task, status task.TaskStatus, code, msg string, cause error, now time.Time) {
	t.Status = status
	t.Progress = 100
	t.CurrentStage = string(status)
	t.CurrentMessage = msg
	t.CompletedAt = &now
	t.UpdatedAt = now
	if code != "" {
		t.ErrorCode = &code
	} else {
		t.ErrorCode = nil
	}
	if status == task.TaskFailed {
		// Persist only the user-safe message. The raw cause may contain host paths,
		// process output or package-derived text; the sanitized cause below is what
		// survives for diagnosis, while the raw error stays in-memory only.
		errorMessage := msg
		t.ErrorMessage = &errorMessage
		t.FailureCause = sanitizedFailureCause(cause)
	} else {
		t.ErrorMessage = nil
		t.FailureCause = nil
	}
}

// sanitizedFailureCause converts a raw error into a persisted-safe cause string:
// host paths and other internal details are stripped, and the result is truncated
// to bound state file size.
func sanitizedFailureCause(cause error) *string {
	if cause == nil {
		return nil
	}
	sanitized := report.SanitizeText(cause.Error())
	const maxCauseRunes = 500
	if runes := []rune(sanitized); len(runes) > maxCauseRunes {
		sanitized = string(runes[:maxCauseRunes])
	}
	if sanitized == "" {
		return nil
	}
	return &sanitized
}

func resetTaskForRetry(t *task.Task) {
	t.Status = task.TaskQueued
	t.PackageProfile = nil
	t.StageResults = nil
	t.ArtifactSummary = nil
	t.RecoveryScore = nil
	t.Diagnostics = nil
	t.ErrorCode = nil
	t.ErrorMessage = nil
	t.FailureCause = nil
	t.Progress = 0
	t.CurrentStage = "queued"
	t.CurrentMessage = "正在从安全检查点重新处理"
	t.CompletedAt = nil
	t.StageStartedAt = nil
	t.StageAttempts = nil
	t.UpdatedAt = time.Now()
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *CompileService) beginStage(ctx context.Context, t *task.Task, status task.TaskStatus, percent int, msg string) {
	t.Status = status
	t.Progress = percent
	t.CurrentStage = string(status)
	t.CurrentMessage = msg
	startedAt := time.Now()
	if t.StageStartedAt == nil {
		t.StageStartedAt = make(map[string]time.Time)
	}
	if t.StageAttempts == nil {
		t.StageAttempts = make(map[string]int)
	}
	t.StageStartedAt[string(status)] = startedAt
	t.StageAttempts[string(status)]++
	t.UpdatedAt = startedAt
	if err := s.repo.Update(ctx, t); err != nil {
		log.Printf("[Task] persist stage start %s failed (%T)", status, err)
	}
	s.publish(t, task.TaskEvent{
		Type:    "progress",
		Stage:   string(status),
		Status:  string(status),
		Percent: percent,
		Message: msg,
		TaskID:  t.ID,
	})
}

func (s *CompileService) finishStage(ctx context.Context, t *task.Task, stage string, success, partial bool, msg string, metrics map[string]interface{}, diagnostics []pkg.Diagnostic) {
	startedAt := time.Now()
	if t.StageStartedAt != nil {
		if ts, ok := t.StageStartedAt[stage]; ok && !ts.IsZero() {
			startedAt = ts
		}
	}
	finishedAt := time.Now()
	attempt := 0
	if t.StageAttempts != nil {
		attempt = t.StageAttempts[stage]
	}
	t.StageResults = append(t.StageResults, task.StageResult{
		Stage:           stage,
		Success:         success,
		Partial:         partial,
		Status:          chooseStageStatus(success, partial),
		StartedAt:       startedAt,
		FinishedAt:      finishedAt,
		DurationMs:      finishedAt.Sub(startedAt).Milliseconds(),
		Attempt:         attempt,
		Engine:          metricString(metrics, "engine"),
		SourceBreakdown: metricIntMap(metrics, "sourceBreakdown"),
		Message:         msg,
		Metrics:         metrics,
		Diagnostics:     diagnostics,
	})
	t.Diagnostics = append(t.Diagnostics, diagnostics...)
	t.UpdatedAt = finishedAt
	if err := s.repo.Update(ctx, t); err != nil {
		log.Printf("[Task] persist stage result %s failed (%T)", stage, err)
	}
}

func (s *CompileService) publish(t *task.Task, event task.TaskEvent) {
	event.TaskID = t.ID
	event.Message = report.SanitizeText(event.Message)
	event.Error = report.SanitizeText(event.Error)
	s.broker.Publish(t.ID, event)
}

func collectArtifactFiles(sourceDir string, known []task.ArtifactFile) []task.ArtifactFile {
	knownMap := make(map[string]task.ArtifactFile, len(known))
	for _, file := range known {
		existing, ok := knownMap[file.Path]
		if !ok || artifactSourceRank(file.Source) > artifactSourceRank(existing.Source) {
			knownMap[file.Path] = file
		}
	}
	var files []task.ArtifactFile
	_ = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return nil
		}
		artifactPath := filepath.ToSlash(filepath.Join("src", rel))
		if knownFile, ok := knownMap[artifactPath]; ok {
			files = append(files, knownFile)
			return nil
		}
		files = append(files, task.ArtifactFile{
			Path:   artifactPath,
			Kind:   strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
			Source: "native",
		})
		return nil
	})
	return dedupeArtifactFiles(files)
}

func hasExtFiles(root, ext string) bool {
	found := false
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ext) {
			found = true
		}
		return nil
	})
	return found
}

func chooseRecoveryMessage(label string, success bool) string {
	if success {
		return label + " 恢复结果已产出"
	}
	return label + " 恢复未形成可用产物"
}

func chooseStageStatus(success, partial bool) string {
	switch {
	case partial:
		return "partial"
	case success:
		return "success"
	default:
		return "failed"
	}
}

func toArtifactFiles(files []recovery.RecoveredFile) []task.ArtifactFile {
	artifacts := make([]task.ArtifactFile, 0, len(files))
	for _, file := range files {
		artifacts = append(artifacts, task.ArtifactFile{
			Path:   filepath.ToSlash(filepath.Join("src", file.Path)),
			Kind:   file.Kind,
			Source: file.Source,
		})
	}
	return artifacts
}

func dedupeArtifactFiles(files []task.ArtifactFile) []task.ArtifactFile {
	seen := make(map[string]task.ArtifactFile, len(files))
	order := make([]string, 0, len(files))
	for _, file := range files {
		existing, ok := seen[file.Path]
		if !ok {
			order = append(order, file.Path)
			seen[file.Path] = file
			continue
		}
		if artifactSourceRank(file.Source) > artifactSourceRank(existing.Source) {
			seen[file.Path] = file
		}
	}
	deduped := make([]task.ArtifactFile, 0, len(order))
	for _, path := range order {
		deduped = append(deduped, seen[path])
	}
	return deduped
}

func artifactSourceRank(source string) int {
	switch source {
	case "manifest", "native", "runtime":
		return 3
	case "generated", "inferred":
		return 2
	case "fallback":
		return 1
	default:
		return 0
	}
}

func dedupeDiagnostics(items []pkg.Diagnostic) []pkg.Diagnostic {
	seen := make(map[string]struct{}, len(items))
	result := make([]pkg.Diagnostic, 0, len(items))
	for _, item := range items {
		key := strings.Join([]string{item.Code, string(item.Severity), item.Stage, item.File, item.Message}, "\x00")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func countRecoveredSources(files []recovery.RecoveredFile) map[string]int {
	breakdown := make(map[string]int)
	for _, file := range files {
		source := file.Source
		if source == "" {
			source = "native"
		}
		breakdown[source]++
	}
	return breakdown
}

func countArtifactSources(files []task.ArtifactFile) map[string]int {
	breakdown := make(map[string]int)
	for _, file := range files {
		source := file.Source
		if source == "" {
			source = "native"
		}
		breakdown[source]++
	}
	return breakdown
}

func metricString(metrics map[string]interface{}, key string) string {
	if metrics == nil {
		return ""
	}
	if value, ok := metrics[key].(string); ok {
		return value
	}
	return ""
}

func metricIntMap(metrics map[string]interface{}, key string) map[string]int {
	if metrics == nil {
		return nil
	}
	value, ok := metrics[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case map[string]int:
		return typed
	case map[string]interface{}:
		out := make(map[string]int, len(typed))
		for k, raw := range typed {
			switch v := raw.(type) {
			case int:
				out[k] = v
			case int64:
				out[k] = int(v)
			case float64:
				out[k] = int(v)
			}
		}
		return out
	default:
		return nil
	}
}

func determineFinalStatus(manifest *verify.ManifestVerifyResult, artifacts *verify.ArtifactVerifyResult, decompilePartial bool) (task.TaskStatus, string, string) {
	if manifest == nil || manifest.PageCount == 0 {
		return task.TaskFailed, "manifest_incomplete", "manifest 恢复结果未通过核心校验"
	}
	if artifacts != nil && artifacts.CriticalFailure {
		return task.TaskFailed, "artifact_critical", "恢复结果存在关键产物缺失或解析失败"
	}
	artifactQualityFailed := artifacts != nil && artifacts.WXMLQualityIssueFiles > 0
	if !manifest.Success || decompilePartial || artifactQualityFailed || artifacts == nil || !artifacts.Success {
		// Verify grades the merged tree independently of the decompile toggle:
		// a package whose pages lack any parseable WXML must never be reported
		// as completed, and the message should say exactly what is missing.
		if artifacts != nil && artifacts.TotalPages > 0 && artifacts.WXMLFiles == 0 {
			// The generic partial branch appends "详见 recovery-report.json" at
			// the call site; keep the specific message free of the suffix.
			return task.TaskPartial, "", "WXML 模板未还原（0 个可解析 WXML 文件）"
		}
		return task.TaskPartial, "", "部分内容需检查"
	}
	return task.TaskCompleted, "", "恢复结果通过核心校验"
}

func (s *CompileService) refreshArtifactSummary(t *task.Task) {
	if t.ArtifactSummary == nil {
		return
	}
	zipPath := filepath.Join(s.cfg.OutputDir, t.ID+".zip")
	if info, err := os.Stat(zipPath); err == nil && !info.IsDir() {
		t.ArtifactSummary.ZipPath = zipPath
		t.ArtifactSummary.ArchiveSize = info.Size()
		t.ArtifactSummary.DownloadReady = true
	}
	t.ArtifactSummary.SourceBreakdown = countArtifactSources(t.ArtifactSummary.Files)
}

func (s *CompileService) syncTaskReports(t *task.Task) error {
	dirs, err := storage.EnsureTaskDirs(s.cfg.TempDir, t.ID)
	if err != nil {
		return err
	}
	reportPath := filepath.Join(dirs.ReportsDir, "recovery-report.json")
	diagnosticsPath := filepath.Join(dirs.ReportsDir, "diagnostics.json")
	packageProfilePath := filepath.Join(dirs.ReportsDir, "package-profile.json")
	artifactsPath := filepath.Join(dirs.ReportsDir, "artifacts.json")

	if t.PackageProfile != nil {
		if s.cfg.ReportEnabled {
			if err := storage.WriteJSON(packageProfilePath, t.PackageProfile); err != nil {
				return err
			}
		}
	}
	if err := report.WriteDiagnostics(diagnosticsPath, t.Diagnostics); err != nil {
		return err
	}
	if t.ArtifactSummary != nil {
		t.ArtifactSummary.ReportPath = reportPath
		t.ArtifactSummary.DiagnosticsPath = diagnosticsPath
		t.ArtifactSummary.ArtifactsPath = artifactsPath
		if s.cfg.ReportEnabled {
			if err := storage.WriteJSON(artifactsPath, t.ArtifactSummary.Files); err != nil {
				return err
			}
		}
	}
	if err := report.WriteRecoveryReport(reportPath, report.BuildRecoveryReport(t)); err != nil {
		return err
	}
	return nil
}
