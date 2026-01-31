import React from 'react';

interface ResultPanelProps {
  fileCount: number;
  downloadUrl: string;
  onReset: () => void;
}

export const ResultPanel: React.FC<ResultPanelProps> = ({
  fileCount,
  downloadUrl,
  onReset,
}) => {
  const handleDownload = () => {
    const a = document.createElement('a');
    a.href = downloadUrl;
    a.download = `wxapkg-${Date.now()}.zip`;
    a.click();
  };

  return (
    <div className="bg-slate-900 rounded-xl border border-slate-700/50 p-6 space-y-5 animate-scale-in">
      {/* Header */}
      <div className="flex items-center gap-4">
        <div className="w-10 h-10 rounded-lg bg-emerald-500/15 flex items-center justify-center">
          <svg className="w-5 h-5 text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
        </div>
        <div>
          <h3 className="font-mono font-semibold text-slate-200">反编译完成</h3>
          <p className="text-xs text-slate-500">
            共提取 {fileCount} 个文件
          </p>
        </div>
      </div>

      {/* File Types */}
      <div className="grid grid-cols-4 gap-2">
        {[
          { ext: '.js', color: 'text-yellow-400' },
          { ext: '.json', color: 'text-blue-400' },
          { ext: '.wxml', color: 'text-green-400' },
          { ext: '.wxss', color: 'text-purple-400' },
        ].map(({ ext, color }) => (
          <div key={ext} className="bg-slate-950/50 rounded px-2 py-1.5 text-center">
            <span className={`text-xs font-mono ${color}`}>{ext}</span>
          </div>
        ))}
      </div>

      {/* Actions */}
      <div className="flex gap-3">
        <button
          onClick={handleDownload}
          className="flex-1 inline-flex items-center justify-center gap-2 px-5 py-2.5 bg-emerald-500 text-slate-950 rounded-lg font-medium transition-all duration-200 hover:bg-emerald-600 hover:shadow-lg hover:shadow-emerald-500/25 active:scale-[0.97] focus-visible:ring-2 focus-visible:ring-emerald-500 focus-visible:ring-offset-2 focus-visible:ring-offset-slate-950"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
          </svg>
          下载结果
        </button>
        <button
          onClick={onReset}
          className="inline-flex items-center justify-center gap-2 px-5 py-2.5 bg-transparent border-2 border-emerald-500 text-emerald-500 rounded-lg font-medium transition-all duration-200 hover:bg-emerald-500/10 active:scale-[0.97] focus-visible:ring-2 focus-visible:ring-emerald-500 focus-visible:ring-offset-2 focus-visible:ring-offset-slate-950"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          继续
        </button>
      </div>
    </div>
  );
};
