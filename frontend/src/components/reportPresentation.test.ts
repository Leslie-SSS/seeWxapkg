import { describe, expect, it } from 'vitest'
import {
  getDiagnosticGroups,
  getEngineCopy,
  getRecoveryMetrics,
  getResultStatusCopy,
  getStageCopy,
  getStageStatusCopy,
} from './reportPresentation'

describe('reportPresentation', () => {
  it('uses truthful labels for backend score fields', () => {
    const metrics = getRecoveryMetrics({
      overall: 68,
      manifest: 100,
      js: 61,
      wxml: 54,
      wxss: 52,
      decompileHit: true,
      fallbackUsed: true,
      generatedRatio: 37,
      fallbackPenalty: 10,
      verifierPassed: false,
    })

    expect(metrics.find((metric) => metric.key === 'missing-page-files')).toMatchObject({
      label: '页面文件不齐率',
      value: '37%',
    })
    expect(metrics.some((metric) => metric.label === '生成占比')).toBe(false)
    expect(metrics.find((metric) => metric.key === 'manifest')).toMatchObject({
      label: '应用配置检查分',
      value: '100/100',
    })
    expect(metrics.find((metric) => metric.key === 'manifest')?.help).toContain('底部导航')
    expect(metrics.find((metric) => metric.key === 'wxml')?.help).toContain('已知异常')
    expect(metrics.find((metric) => metric.key === 'verifier')?.help).toContain('不运行小程序')
    expect(getEngineCopy('static-verifier')).toBe('静态质量检查')
  })

  it('translates result and stage states into user language', () => {
    expect(getResultStatusCopy('partial').title).toBe('反编译结果已生成，部分内容需检查')
    expect(getResultStatusCopy('partial').description).toBe('结果可以下载，建议先查看下方提示。')
    expect(getStageCopy('fallback_recovering').label).toBe('尝试补全内容')
    expect(
      getStageStatusCopy({
        stage: 'recovering_wxml',
        success: false,
        partial: true,
        status: 'partial',
      }).label
    ).toBe('需检查')
  })

  it('groups repeated technical diagnostics into actionable summaries', () => {
    const groups = getDiagnosticGroups([
      {
        code: 'recover.js.page.missing_runtime',
        severity: 'warn',
        message: 'raw message',
        file: 'pages/a.js',
      },
      {
        code: 'recover.js.page.missing_runtime',
        severity: 'warn',
        message: 'raw message',
        file: 'pages/b.js',
      },
      {
        code: 'fallback.wxss.data_parse_failed',
        severity: 'warn',
        message: 'raw message',
      },
      {
        code: 'classifier.variant',
        severity: 'info',
        message: 'not actionable',
      },
    ])

    expect(groups).toHaveLength(2)
    expect(groups[0]).toMatchObject({ key: 'page-logic', count: 2 })
    expect(groups[1]).toMatchObject({ key: 'runtime-style', count: 1 })
  })

  it('explains new recovery-quality diagnostics without overstating dynamic events', () => {
    const groups = getDiagnosticGroups([
      {
        code: 'fallback.wxml.unresolved_fragments',
        severity: 'warn',
        message: 'raw message',
      },
      {
        code: 'verify.wxml.unresolved_recovery_marker',
        severity: 'warn',
        message: 'raw verifier message',
      },
      {
        code: 'verify.wxml.suspicious_event_binding',
        severity: 'warn',
        message: 'raw message',
      },
      {
        code: 'verify.wxml.dynamic_event_binding',
        severity: 'warn',
        message: 'raw message',
      },
      {
        code: 'verify.manifest.tabbar_page_path_invalid',
        severity: 'warn',
        message: 'raw message',
      },
    ])

    expect(groups.map((group) => group.key)).toEqual([
      'unresolved-structure',
      'event-binding',
      'dynamic-event-binding',
      'app-config',
    ])
    expect(groups.find((group) => group.key === 'unresolved-structure')?.count).toBe(2)
    expect(groups.find((group) => group.key === 'dynamic-event-binding')?.description).toContain(
      '可能是合法'
    )
  })
})
