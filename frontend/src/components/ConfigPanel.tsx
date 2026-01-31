import React from 'react';

interface ConfigPanelProps {
  appId: string;
  setAppId: (value: string) => void;
  beautify: boolean;
  setBeautify: (value: boolean) => void;
  disabled?: boolean;
}

export const ConfigPanel: React.FC<ConfigPanelProps> = ({
  appId,
  setAppId,
  beautify,
  setBeautify,
  disabled = false,
}) => {
  return (
    <div className="bg-slate-900 rounded-xl border border-slate-800 p-4 space-y-4">
      {/* AppID Input */}
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <label htmlFor="appId" className="text-xs font-medium text-slate-500 uppercase tracking-wide">
            小程序 AppID
          </label>
          {appId && (
            <button
              onClick={() => setAppId('')}
              disabled={disabled}
              className="text-xs text-slate-500 hover:text-red-400 transition-colors"
            >
              清除
            </button>
          )}
        </div>
        <input
          id="appId"
          type="text"
          value={appId}
          onChange={(e) => setAppId(e.target.value)}
          placeholder="wxXXXXXXXXXXXXXXX"
          disabled={disabled}
          className="w-full px-4 py-2.5 bg-slate-950/50 border border-slate-700 rounded-lg text-slate-50 placeholder-slate-500 font-mono text-sm transition-all duration-200 focus:border-emerald-500 focus:ring-2 focus:ring-emerald-500/20 focus:outline-none disabled:opacity-50 disabled:cursor-not-allowed"
        />
        <p className="text-xs text-slate-600">
          加密包必填，普通包可选
        </p>
        <div className="mt-2 p-2 bg-slate-950/50 rounded-lg border border-slate-800">
          <p className="text-xs text-slate-500 mb-1">如何获取 AppID：</p>
          <ol className="text-xs text-slate-600 space-y-1 list-decimal list-inside">
            <li>打开小程序任意页面</li>
            <li>点击右上角 •••</li>
            <li>点击"更多资料"或"转发"</li>
            <li>在页面路径中可找到 AppID</li>
          </ol>
        </div>
      </div>

      {/* Beautify Toggle */}
      <div className="flex items-center justify-between py-1">
        <div>
          <label htmlFor="beautify" className="text-xs font-medium text-slate-400 uppercase tracking-wide">
            代码美化
          </label>
          <p className="text-xs text-slate-600">格式化输出代码</p>
        </div>
        <button
          id="beautify"
          type="button"
          role="switch"
          aria-checked={beautify}
          onClick={() => !disabled && setBeautify(!beautify)}
          disabled={disabled}
          className={`relative inline-flex items-center cursor-pointer ${disabled ? 'opacity-50 cursor-not-allowed' : ''}`}
        >
          <div className={`w-11 h-6 rounded-full transition-colors duration-200 ${beautify ? 'bg-emerald-500' : 'bg-slate-800'}`} />
          <div className={`absolute top-0.5 left-0.5 w-5 h-5 bg-white rounded-full shadow transition-transform duration-200 ${beautify ? 'translate-x-5' : ''}`} />
          <span className="sr-only">切换代码美化</span>
        </button>
      </div>
    </div>
  );
};
