import React from 'react';

interface ProgressBarProps {
  progress: number;
  stage: string;
  message: string;
}

const stages = [
  { key: 'upload', label: '上传' },
  { key: 'decrypt', label: '解密' },
  { key: 'unpack', label: '解包' },
  { key: 'pack', label: '打包' },
];

export const ProgressBar: React.FC<ProgressBarProps> = ({ progress, stage, message }) => {
  const isError = stage === 'error';

  const getStepStatus = (key: string) => {
    if (isError) return 'error';
    if (stage === 'completed') return 'done';
    if (stage.includes(key)) return 'active';
    const currentIndex = stages.findIndex((s) => stage.includes(s.key));
    const stepIndex = stages.findIndex((s) => s.key === key);
    if (currentIndex > -1 && stepIndex < currentIndex) return 'done';
    return 'pending';
  };

  return (
    <div className="bg-slate-900 rounded-xl border border-slate-800 p-4 space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between text-sm">
        <span className="text-slate-500">
          {stages.find((s) => stage.includes(s.key))?.label ?? stage}
        </span>
        <span className={`font-mono ${isError ? 'text-red-400' : 'text-emerald-400'}`}>
          {Math.round(progress)}%
        </span>
      </div>

      {/* Progress Bar */}
      <div className="w-full h-1.5 bg-slate-800 rounded-full overflow-hidden">
        <div
          className={`h-full rounded-full transition-all duration-300 ease-out ${isError ? 'bg-red-500' : 'bg-gradient-to-r from-emerald-500 to-emerald-400'}`}
          style={{ width: `${progress}%` }}
          role="progressbar"
          aria-valuenow={progress}
          aria-valuemin={0}
          aria-valuemax={100}
        />
      </div>

      {/* Message */}
      <p className={`text-xs ${isError ? 'text-red-400' : 'text-slate-500'}`}>{message}</p>

      {/* Steps */}
      <div className="flex items-center gap-2">
        {stages.map((s, i) => {
          const status = getStepStatus(s.key);
          return (
            <React.Fragment key={s.key}>
              <div className="flex items-center gap-2">
                <div
                  className={`w-1.5 h-1.5 rounded-full ${
                    status === 'done'
                      ? 'bg-emerald-500'
                      : status === 'active'
                      ? 'bg-emerald-500 animate-pulse'
                      : 'bg-slate-700'
                  }`}
                />
                <span className={`text-xs ${status === 'active' ? 'text-slate-300' : 'text-slate-600'}`}>
                  {s.label}
                </span>
              </div>
              {i < stages.length - 1 && <div className="flex-1 h-px bg-slate-800" />}
            </React.Fragment>
          );
        })}
      </div>
    </div>
  );
};
