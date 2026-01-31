import { useState } from 'react';

interface PathCopyProps {
  platform: string;
  path: string;
}

export function PathCopy({ platform, path }: PathCopyProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(path);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      // Fallback for older browsers or non-secure contexts
      const textArea = document.createElement('textarea');
      textArea.value = path;
      textArea.style.position = 'fixed';
      textArea.style.opacity = '0';
      document.body.appendChild(textArea);
      textArea.focus();
      textArea.select();
      try {
        document.execCommand('copy');
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      } catch (e) {
        console.error('Copy failed', e);
      }
      document.body.removeChild(textArea);
    }
  };

  return (
    <div className="flex items-center gap-2 group" title={path}>
      <span className="text-slate-500 flex-shrink-0 text-xs">{platform}:</span>
      <code
        className="font-mono text-slate-400 text-xs block truncate cursor-help"
        style={{ maxWidth: '200px' }}
      >
        {path}
      </code>
      <button
        onClick={handleCopy}
        className="flex-shrink-0 p-1 rounded text-slate-500 hover:text-emerald-400 hover:bg-slate-800 transition-colors"
        title={copied ? '已复制' : '复制路径'}
      >
        {copied ? (
          <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
        ) : (
          <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
          </svg>
        )}
      </button>
    </div>
  );
}
