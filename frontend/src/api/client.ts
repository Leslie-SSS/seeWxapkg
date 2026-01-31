const API_BASE = '/api';

export interface CompileRequest {
  file: File;
  appId?: string;
  beautify?: boolean;
}

export interface ProgressEvent {
  type: 'progress' | 'complete' | 'error';
  stage: string;
  percent: number;
  message: string;
  fileCount?: number;
  taskId?: string;
  downloadUrl?: string;
  error?: string;
}

export interface CompileResponse {
  success: boolean;
  taskId: string;
  message: string;
  downloadUrl?: string;
}

export class ApiClient {
  private base: string;

  constructor(base: string = API_BASE) {
    this.base = base;
  }

  async compile(request: CompileRequest): Promise<CompileResponse> {
    const formData = new FormData();
    formData.append('file', request.file);
    if (request.appId) {
      formData.append('appId', request.appId);
    }
    if (request.beautify !== undefined) {
      formData.append('beautify', request.beautify.toString());
    }

    const response = await fetch(`${this.base}/compile`, {
      method: 'POST',
      body: formData,
    });

    if (!response.ok) {
      throw new Error('Upload failed');
    }

    return response.json();
  }

  subscribeProgress(taskId: string, onEvent: (event: ProgressEvent) => void): () => void {
    const eventSource = new EventSource(
      `${this.base}/events?taskId=${taskId}`
    );

    eventSource.onmessage = (e) => {
      try {
        const event = JSON.parse(e.data) as ProgressEvent;
        onEvent(event);

        if (event.type === 'complete' || event.type === 'error') {
          eventSource.close();
        }
      } catch {
        // 静默处理解析错误，避免暴露内部信息
      }
    };

    eventSource.onerror = () => {
      // 静默处理错误，关闭连接
      eventSource.close();
    };

    return () => eventSource.close();
  }

  getDownloadUrl(taskId: string): string {
    return `/api/download/${taskId}`;
  }

  async healthCheck(): Promise<{ status: string; version: string }> {
    const response = await fetch(`${this.base}/health`);
    return response.json();
  }
}

export const api = new ApiClient();
