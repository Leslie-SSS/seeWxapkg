import { useState, useCallback, useRef } from 'react';

interface FileUploaderProps {
  onFileSelect: (file: File) => void;
  disabled?: boolean;
  maxSize?: number;
}

export const FileUploader: React.FC<FileUploaderProps> = ({
  onFileSelect,
  disabled = false,
  maxSize = 50 * 1024 * 1024,
}) => {
  const [isDragging, setIsDragging] = useState(false);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [error, setError] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  const validate = (file: File): string => {
    if (!file.name.toLowerCase().endsWith('.wxapkg')) {
      return '文件格式错误，请选择 .wxapkg 文件';
    }
    if (file.size > maxSize) {
      return `文件过大，最大支持 ${formatSize(maxSize)}`;
    }
    return '';
  };

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    if (!disabled) setIsDragging(true);
  }, [disabled]);

  const handleDragLeave = useCallback(() => {
    setIsDragging(false);
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setIsDragging(false);

      if (disabled) return;

      const file = e.dataTransfer.files[0];
      const err = validate(file);
      if (err) {
        setError(err);
        setSelectedFile(null);
        return;
      }

      setError('');
      setSelectedFile(file);
      onFileSelect(file);
    },
    [disabled, maxSize, onFileSelect]
  );

  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      if (disabled || !e.target.files?.[0]) return;

      const file = e.target.files[0];
      const err = validate(file);
      if (err) {
        setError(err);
        setSelectedFile(null);
        return;
      }

      setError('');
      setSelectedFile(file);
      onFileSelect(file);
    },
    [disabled, maxSize, onFileSelect]
  );

  const formatSize = (bytes: number): string => {
    const units = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
  };

  return (
    <div
      className={`relative overflow-hidden rounded-lg border-2 border-dashed transition-all duration-200 ${
        error
          ? 'border-red-500 bg-red-500/5'
          : isDragging
          ? 'border-emerald-500 bg-emerald-500/5'
          : 'border-slate-700 hover:border-emerald-500/40 hover:bg-slate-900/50'
      } ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}`}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
      onClick={() => !disabled && inputRef.current?.click()}
      role="button"
      tabIndex={disabled ? -1 : 0}
      aria-label="Upload wxapkg file"
    >
      <input
        ref={inputRef}
        type="file"
        accept=".wxapkg"
        onChange={handleChange}
        disabled={disabled}
        className="sr-only"
      />

      <div className="p-8 text-center space-y-4">
        {/* Icon */}
        <div className="flex justify-center">
          <div
            className={`w-14 h-14 rounded-lg flex items-center justify-center transition-all ${
              error
                ? 'bg-red-500/10'
                : isDragging
                ? 'bg-emerald-500/20 scale-105'
                : 'bg-slate-800'
            }`}
          >
            {selectedFile && !error ? (
              <svg className="w-7 h-7 text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
              </svg>
            ) : (
              <svg
                className={`w-7 h-7 ${error ? 'text-red-400' : 'text-slate-500'}`}
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12"
                />
              </svg>
            )}
          </div>
        </div>

        {/* Text */}
        {selectedFile && !error ? (
          <div className="animate-slide-up">
            <p className="font-mono text-sm text-emerald-400">{selectedFile.name}</p>
            <p className="text-xs text-slate-500 mt-1">{formatSize(selectedFile.size)}</p>
          </div>
        ) : error ? (
          <p className="text-sm text-red-400">{error}</p>
        ) : (
          <div>
            <p className="text-sm text-slate-300">
              拖拽 <span className="font-mono text-sm bg-slate-800 px-1.5 py-0.5 rounded text-slate-400">.wxapkg</span> 文件到此处
            </p>
            <p className="text-xs text-slate-500 mt-1">或点击选择文件</p>
          </div>
        )}

        {/* Hint */}
        {!selectedFile && !error && (
          <p className="text-xs text-slate-600">
            最大支持 {formatSize(maxSize)}
          </p>
        )}
      </div>
    </div>
  );
};
