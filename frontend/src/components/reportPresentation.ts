import type { Diagnostic, PackageProfile, RecoveryScore, StageResult } from '../api/client'

export type ResultStatus = 'completed' | 'partial' | 'failed'
export type ReportTone = 'success' | 'warning' | 'danger' | 'neutral'

export function getResultStatusCopy(status: ResultStatus) {
  if (status === 'partial') {
    return {
      title: '反编译结果已生成，部分内容需检查',
      badge: '可下载 · 需检查',
      tone: 'warning' as const,
      description: '结果可以下载，建议先查看下方提示。',
    }
  }

  if (status === 'failed') {
    return {
      title: '反编译未完成',
      badge: '未完成',
      tone: 'danger' as const,
      description: '请根据提示调整后重试。',
    }
  }

  return {
    title: '反编译结果已生成',
    badge: '可下载',
    tone: 'success' as const,
    description: '建议在微信开发者工具中检查关键页面。',
  }
}

const STAGE_COPY: Record<string, { label: string; description: string }> = {
  uploading: { label: '上传文件', description: '把文件发送到处理服务' },
  processing: { label: '准备反编译', description: '创建任务并准备资源' },
  queued: { label: '等待处理', description: '等待空闲处理资源' },
  classifying: { label: '识别文件', description: '识别加密、分包和运行时特征' },
  decrypting: { label: '检查并解密文件', description: '检查加密状态，必要时使用 AppID 解密' },
  unpacking: { label: '解包文件', description: '提取 wxapkg 内的文件' },
  normalizing: { label: '整理文件结构', description: '整理页面、脚本、样式和配置的对应关系' },
  recovering_manifest: { label: '整理应用配置', description: '整理配置和页面清单' },
  recovering_js: { label: '还原程序逻辑', description: '生成页面逻辑文件' },
  recovering_wxml: { label: '还原页面结构', description: '生成页面结构文件' },
  recovering_wxss: { label: '还原页面样式', description: '生成页面样式文件' },
  fallback_recovering: {
    label: '尝试补全内容',
    description: '有缺口时尝试辅助方案',
  },
  formatting: { label: '整理代码格式', description: '调整代码排版，提升阅读体验' },
  verifying: { label: '检查文件结构与语法', description: '检查文件是否齐全、语法和引用是否可解析' },
  packaging: { label: '生成下载文件', description: '压缩并生成本次处理结果' },
  completed: { label: '结果已生成', description: '反编译结果可以下载' },
  partial: { label: '部分内容需检查', description: '结果已生成，部分内容需人工检查' },
  failed: { label: '反编译未完成', description: '遇到无法继续的问题' },
}

export function getStageCopy(stage: string) {
  return STAGE_COPY[stage] ?? { label: '处理当前步骤', description: '正在处理相关文件' }
}

export function getStageStatusCopy(stage: StageResult): { label: string; tone: ReportTone } {
  if (stage.partial || stage.status === 'partial') {
    return { label: '需检查', tone: 'warning' }
  }
  if (stage.success || stage.status === 'success') {
    return { label: '已完成', tone: 'success' }
  }
  if (stage.status === 'queued' || stage.status === 'pending') {
    return { label: '等待中', tone: 'neutral' }
  }
  if (stage.status === 'running' || stage.status === 'processing') {
    return { label: '进行中', tone: 'neutral' }
  }
  return { label: '未完成', tone: 'danger' }
}

export function formatDuration(durationMs?: number) {
  if (durationMs === undefined) return ''
  if (durationMs < 1000) return `${durationMs} 毫秒`
  if (durationMs < 60_000) {
    return `${(durationMs / 1000).toFixed(durationMs < 10_000 ? 1 : 0)} 秒`
  }
  const minutes = Math.floor(durationMs / 60_000)
  const seconds = Math.round((durationMs % 60_000) / 1000)
  return seconds ? `${minutes} 分 ${seconds} 秒` : `${minutes} 分钟`
}

const ENGINE_COPY: Record<string, string> = {
  native: '内置反编译',
  fallback: '辅助反编译',
  wxappUnpacker: '辅助反编译',
  'safe-format': '代码格式整理',
  parser: '语法与引用检查',
  'static-verifier': '静态质量检查',
  'subpackage-guard': '分包保护模式',
  disabled: '未启用',
}

export function getEngineCopy(engine?: string) {
  if (!engine) return ''
  return ENGINE_COPY[engine] ?? engine
}

interface ReportMetric {
  key: string
  label: string
  value: string
  help: string
  tone?: ReportTone
}

export const SCORE_TRUTH_NOTE = '这是静态质量分，不是源码还原率，也不代表项目可直接运行。'

export function getRecoveryMetrics(score: RecoveryScore): ReportMetric[] {
  return [
    {
      key: 'manifest',
      label: '应用配置检查分',
      value: `${score.manifest}/100`,
      help: '检查页面清单、路径和底部导航；不判断内容是否与原包一致。',
    },
    {
      key: 'js',
      label: '程序逻辑检查分',
      value: `${score.js}/100`,
      help: '检查 JS 能否被语法解析，不会执行代码或验证业务逻辑。',
    },
    {
      key: 'wxml',
      label: '页面结构检查分',
      value: `${score.wxml}/100`,
      help: '检查 WXML 语法、模板引用和已知异常；不判断页面是否与原程序一致。',
    },
    {
      key: 'wxss',
      label: '页面样式检查分',
      value: `${score.wxss}/100`,
      help: '检查 WXSS 能否解析，不判断最终视觉效果。',
    },
    {
      key: 'missing-page-files',
      label: '页面文件不齐率',
      value: `${score.generatedRatio ?? 0}%`,
      help: '页面清单中缺少同名 JS 或 WXML 的比例，越低越好。',
      tone: score.generatedRatio > 0 ? 'warning' : 'success',
    },
    {
      key: 'verifier',
      label: '语法与引用检查',
      value: score.verifierPassed ? '未发现阻断问题' : '发现需检查项',
      help: '只做静态检查，不运行小程序。',
      tone: score.verifierPassed ? 'success' : 'warning',
    },
    {
      key: 'fallback',
      label: '辅助反编译',
      value: score.fallbackUsed ? '已使用，建议重点检查' : '未使用',
      help: '标准反编译存在缺口时会启用辅助方案；已使用不等于失败。',
      tone: score.fallbackUsed ? 'warning' : 'neutral',
    },
    {
      key: 'deep-recovery',
      label: '深度反编译',
      value: score.decompileHit ? '本次已启用' : '本次未启用',
      help: '仅表示本次是否请求深度反编译，不代表结果完整。',
    },
    ...(score.fallbackPenalty > 0
      ? [
          {
            key: 'fallback-adjustment',
            label: '辅助反编译分数调整',
            value: `-${score.fallbackPenalty} 分`,
            help: '因使用辅助方案而降低静态参考分，反映文件来源的不确定性。',
            tone: 'neutral' as const,
          },
        ]
      : []),
  ]
}

const VARIANT_COPY: Record<string, string> = {
  standard: '标准小程序包',
  encrypted: '加密小程序包',
  wechat4x: '微信 4.x 格式小程序包',
  subpackage: '独立分包',
  game: '小游戏包',
  unknown: '暂未识别具体类型',
}

export function getPackagePresentation(profile: PackageProfile) {
  const features: string[] = []
  if (profile.isEncrypted) features.push('上传文件已加密')
  if (profile.isStandardWxapkg) features.push('wxapkg 结构有效')
  if (profile.isSubPackage) features.push('检测到独立分包')
  if (profile.isGamePackage) features.push('检测到小游戏配置')
  if (profile.isWeChat4xLike) features.push('包含运行时聚合文件')
  if (profile.hasAppConfigJSON) features.push('发现应用配置')
  if (profile.hasAppServiceJS) features.push('发现主逻辑包')
  if (profile.hasWorkersJS) features.push('发现 Worker 脚本')
  if (profile.hasPageFrameHTML || profile.hasPageFrameJS) features.push('发现页面运行时文件')
  if (profile.hasAppWxssJS) features.push('发现样式运行时文件')
  if (profile.indexFileCount > 0) features.push(`包内索引记录 ${profile.indexFileCount} 条`)

  return {
    variant: VARIANT_COPY[profile.suspectedVariant] ?? VARIANT_COPY.unknown,
    features: [...new Set(features)],
  }
}

export function formatBytes(bytes?: number) {
  if (bytes === undefined || bytes < 0) return ''
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

interface DiagnosticGroup {
  key: string
  label: string
  description: string
  count: number
  tone: 'warning' | 'danger'
}

const DIAGNOSTIC_COPY: Array<{
  matches: (diagnostic: Diagnostic) => boolean
  key: string
  label: string
  description: string
}> = [
  {
    matches: (diagnostic) => diagnostic.code === 'recover.js.app.missing',
    key: 'app-entry',
    label: '应用入口需要检查',
    description: '应用入口未能直接反编译，结果中保留了可用的运行时线索。',
  },
  {
    matches: (diagnostic) => diagnostic.code.startsWith('recover.js.page.'),
    key: 'page-logic',
    label: '页面逻辑需要检查',
    description: '这些页面的逻辑文件未能直接反编译，建议运行对应页面确认功能。',
  },
  {
    matches: (diagnostic) =>
      diagnostic.code === 'fallback.wxml.unresolved_fragments' ||
      diagnostic.code === 'verify.wxml.unresolved_recovery_marker',
    key: 'unresolved-structure',
    label: '页面有少量内容无法安全还原',
    description: '系统没有猜测未知内容，也没有放入可见占位；请结合原始运行时代码检查对应页面。',
  },
  {
    matches: (diagnostic) =>
      diagnostic.code === 'fallback.wxml.suspicious_event_bindings' ||
      diagnostic.code === 'verify.wxml.suspicious_event_binding',
    key: 'event-binding',
    label: '部分页面交互需要检查',
    description: '有些事件绑定不像明确的处理函数名，建议实际点击对应控件确认交互。',
  },
  {
    matches: (diagnostic) => diagnostic.code === 'verify.wxml.dynamic_event_binding',
    key: 'dynamic-event-binding',
    label: '页面使用了动态事件绑定',
    description: '这种写法可能是合法的动态处理函数，静态检查无法确认运行时取值，建议运行页面确认。',
  },
  {
    matches: (diagnostic) => diagnostic.code === 'verify.wxml.placeholder_sentinel',
    key: 'recovery-placeholder',
    label: '页面仍含未还原内容',
    description: '文件虽然能解析，但页面内容并未完整反编译，需要优先人工处理。',
  },
  {
    matches: (diagnostic) =>
      diagnostic.code === 'verify.wxml.unparsable' || diagnostic.code === 'verify.wxml.missing_ref',
    key: 'page-structure-check',
    label: '页面结构检查未完全通过',
    description: '部分页面结构无法解析或引用的模板不存在，建议先修复这些文件。',
  },
  {
    matches: (diagnostic) => diagnostic.code.startsWith('recover.wxml.'),
    key: 'page-structure',
    label: '页面结构需要检查',
    description: '这些页面的结构文件未能直接反编译，建议检查页面内容和交互。',
  },
  {
    matches: (diagnostic) => diagnostic.code === 'fallback.wxml.opcode_skipped',
    key: 'dynamic-structure',
    label: '动态页面结构无法完全静态解析',
    description: '页面中包含运行时才确定的动态表达式，可能需要人工补充。',
  },
  {
    matches: (diagnostic) => diagnostic.code === 'fallback.wxss.data_parse_failed',
    key: 'runtime-style',
    label: '部分页面样式需要检查',
    description: '一部分运行时样式数据无法静态解析，建议重点检查对应页面的显示效果。',
  },
  {
    matches: (diagnostic) => diagnostic.code.startsWith('recover.fallback.'),
    key: 'fallback-gap',
    label: '辅助反编译仍有缺口',
    description: '辅助方案已补全部分文件，但仍有内容需要人工检查。',
  },
  {
    matches: (diagnostic) =>
      diagnostic.code.startsWith('manifest.') || diagnostic.code.startsWith('verify.manifest.'),
    key: 'app-config',
    label: '应用配置需要检查',
    description: '页面清单或底部导航中存在无效、重复或找不到对应页面的配置。',
  },
  {
    matches: (diagnostic) => diagnostic.code === 'verify.artifacts.partial',
    key: 'page-files',
    label: '部分页面文件不齐全',
    description: '页面缺少同名的 JS 或 WXML 文件，下载后需要补充或确认。',
  },
  {
    matches: (diagnostic) =>
      diagnostic.code === 'verify.js.unparsable' || diagnostic.code === 'verify.wxss.unparsable',
    key: 'source-parse',
    label: '部分源码无法通过语法检查',
    description: '相应的程序逻辑或样式文件需要修正后再使用。',
  },
  {
    matches: (diagnostic) => diagnostic.code.startsWith('format.files.'),
    key: 'formatting',
    label: '部分文件未完成排版整理',
    description: '原文件已被保留，不会因美化失败而丢失内容；可在下载后手动整理。',
  },
]

export function getDiagnosticGroups(diagnostics: Diagnostic[]): DiagnosticGroup[] {
  const groups = new Map<string, DiagnosticGroup>()

  for (const diagnostic of diagnostics) {
    if (diagnostic.severity === 'info') continue
    const copy = DIAGNOSTIC_COPY.find((item) => item.matches(diagnostic))
    const key = copy?.key ?? `other-${diagnostic.severity}`
    const existing = groups.get(key)
    if (existing) {
      existing.count += 1
      if (diagnostic.severity === 'error') existing.tone = 'danger'
      continue
    }

    groups.set(key, {
      key,
      label:
        copy?.label ?? (diagnostic.severity === 'error' ? '存在未通过的检查' : '其他内容建议检查'),
      description: copy?.description ?? '这类提示暂时没有通俗说明，请在技术报告中查看原始记录。',
      count: 1,
      tone: diagnostic.severity === 'error' ? 'danger' : 'warning',
    })
  }

  return [...groups.values()].sort((left, right) => {
    if (left.tone !== right.tone) return left.tone === 'danger' ? -1 : 1
    return right.count - left.count
  })
}
