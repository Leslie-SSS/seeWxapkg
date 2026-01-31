import { useState, useCallback, useRef } from 'react';
import { api, ProgressEvent } from '../api/client';

interface UploadState {
  isUploading: boolean;
  progress: number;
  stage: string;
  message: string;
  fileCount?: number;
  downloadUrl?: string;
  error?: string;
  taskId?: string;
  isComplete: boolean;
}

export function useSeeWxapkgUpload() {
  const [state, setState] = useState<UploadState>({
    isUploading: false,
    progress: 0,
    stage: '',
    message: '',
    isComplete: false,
  });

  const unsubscribeRef = useRef<(() => void) | null>(null);

  const upload = useCallback(async (file: File, appId?: string, beautify = true) => {
    // 清理之前的订阅
    if (unsubscribeRef.current) {
      unsubscribeRef.current();
    }

    setState({
      isUploading: true,
      progress: 0,
      stage: 'uploading',
      message: '正在上传文件...',
      isComplete: false,
    });

    try {
      // 上传文件
      const response = await api.compile({ file, appId, beautify });

      if (!response.success) {
        throw new Error(response.message);
      }

      setState((prev) => ({
        ...prev,
        taskId: response.taskId,
        stage: 'processing',
        message: '开始处理...',
      }));

      // 订阅进度
      unsubscribeRef.current = api.subscribeProgress(
        response.taskId,
        (event: ProgressEvent) => {
          if (event.type === 'progress') {
            setState((prev) => ({
              ...prev,
              progress: event.percent,
              stage: event.stage,
              message: event.message,
            }));
          } else if (event.type === 'complete') {
            setState({
              isUploading: false,
              progress: 100,
              stage: 'completed',
              message: event.message,
              fileCount: event.fileCount,
              downloadUrl: event.taskId ? api.getDownloadUrl(event.taskId) : undefined,
              taskId: event.taskId,
              isComplete: true,
            });
          } else if (event.type === 'error') {
            setState({
              isUploading: false,
              progress: 0,
              stage: 'error',
              message: event.message,
              error: event.error,
              isComplete: false,
            });
          }
        }
      );
    } catch (err) {
      setState({
        isUploading: false,
        progress: 0,
        stage: 'error',
        message: '上传失败，请重试',
        error: err instanceof Error ? err.message : '未知错误',
        isComplete: false,
      });
    }
  }, []);

  const reset = useCallback(() => {
    if (unsubscribeRef.current) {
      unsubscribeRef.current();
      unsubscribeRef.current = null;
    }
    setState({
      isUploading: false,
      progress: 0,
      stage: '',
      message: '',
      isComplete: false,
    });
  }, []);

  return {
    ...state,
    upload,
    reset,
  };
}
