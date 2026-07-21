/**
 * HTTP wrapper around the beautify core.
 *
 * CPU-heavy AST work runs in a bounded worker pool so one large bundle cannot
 * block health checks or every other request. Jobs have an end-to-end deadline;
 * timed-out workers are terminated rather than continuing invisible work.
 */

const http = require('http');
const { isMainThread, parentPort, Worker } = require('worker_threads');

const { beautify, createResult, getRuntimeConfig } = require('./core');

const PORT = Number.parseInt(process.env.BEAUTIFY_PORT || '3001', 10);
const HOST = process.env.BEAUTIFY_HOST || '127.0.0.1';

function positiveInteger(value, fallback) {
  return Number.isSafeInteger(value) && value > 0 ? value : fallback;
}

function normalizeConfig(input = {}) {
  const defaults = getRuntimeConfig();
  return {
    ...defaults,
    ...input,
    jobTimeoutMs: positiveInteger(input.jobTimeoutMs, defaults.jobTimeoutMs),
    maxContentSize: positiveInteger(input.maxContentSize, defaults.maxContentSize),
    queueSize: positiveInteger(input.queueSize, defaults.queueSize),
    workerCount: positiveInteger(input.workerCount, defaults.workerCount),
  };
}

function writeJson(res, statusCode, payload) {
  if (res.destroyed || res.writableEnded) return;
  res.writeHead(statusCode, {
    'Content-Type': 'application/json; charset=utf-8',
    'Cache-Control': 'no-store',
  });
  res.end(JSON.stringify(payload));
}

function bodyLimitForContent(maxContentSize) {
  // A UTF-8 content byte can expand to six JSON bytes (`\\u00xx`). Leave room
  // for the request envelope while retaining a strict, deterministic ceiling.
  return Math.min(Number.MAX_SAFE_INTEGER, maxContentSize * 6 + 65536);
}

function readJsonBody(req, maxBodySize) {
  return new Promise((resolve, reject) => {
    const declaredLength = Number.parseInt(req.headers['content-length'] || '', 10);
    if (Number.isSafeInteger(declaredLength) && declaredLength > maxBodySize) {
      const error = new Error('Request body too large');
      error.statusCode = 413;
      req.resume();
      reject(error);
      return;
    }

    const chunks = [];
    let received = 0;
    let settled = false;

    const fail = error => {
      if (settled) return;
      settled = true;
      reject(error);
    };

    req.on('data', chunk => {
      if (settled) return;
      received += chunk.length;
      if (received > maxBodySize) {
        const error = new Error('Request body too large');
        error.statusCode = 413;
        fail(error);
        return;
      }
      chunks.push(chunk);
    });

    req.on('end', () => {
      if (settled) return;
      settled = true;
      try {
        resolve(JSON.parse(Buffer.concat(chunks, received).toString('utf8')));
      } catch (error) {
        error.statusCode = 400;
        reject(error);
      }
    });

    req.on('error', fail);
    req.on('aborted', () => {
      const error = new Error('Request aborted');
      error.statusCode = 400;
      fail(error);
    });
  });
}

class BeautifyWorkerPool {
  constructor(config) {
    this.config = config;
    this.closed = false;
    this.nextID = 1;
    this.queue = [];
    this.slots = new Set();

    for (let index = 0; index < config.workerCount; index += 1) this.spawn();
  }

  spawn() {
    if (this.closed) return;
    const worker = new Worker(__filename);
    worker.unref();
    const slot = { busy: false, job: null, worker, replacing: false };
    this.slots.add(slot);

    worker.on('message', message => {
      const job = slot.job;
      if (!job || message.id !== job.id) return;
      this.finish(slot, null, message.result);
    });
    worker.on('error', error => this.replace(slot, error));
    worker.on('exit', code => {
      if (!slot.replacing && !this.closed && code !== 0) {
        this.replace(slot, new Error(`Beautify worker exited with code ${code}`));
      }
    });
  }

  stats() {
    let busy = 0;
    for (const slot of this.slots) if (slot.busy) busy += 1;
    return {
      busy,
      queued: this.queue.length,
      workers: this.slots.size,
    };
  }

  submit(payload) {
    if (this.closed) return Promise.reject(new Error('Beautify worker pool is closed'));
    if (this.queue.length >= this.config.queueSize && [...this.slots].every(slot => slot.busy)) {
      return Promise.reject(new Error('Beautify queue is full'));
    }

    return new Promise((resolve, reject) => {
      const job = {
        id: this.nextID++,
        payload,
        reject,
        resolve,
        settled: false,
        slot: null,
        timer: null,
      };
      job.timer = setTimeout(() => this.timeout(job), this.config.jobTimeoutMs);
      this.queue.push(job);
      this.drain();
    });
  }

  settle(job, error, result) {
    if (job.settled) return;
    job.settled = true;
    clearTimeout(job.timer);
    if (error) job.reject(error);
    else job.resolve(result);
  }

  timeout(job) {
    if (job.settled) return;
    const error = new Error(`Beautify job timed out after ${this.config.jobTimeoutMs}ms`);

    if (job.slot) {
      this.replace(job.slot, error);
      return;
    }

    const index = this.queue.indexOf(job);
    if (index !== -1) this.queue.splice(index, 1);
    this.settle(job, error);
  }

  drain() {
    if (this.closed) return;
    for (const slot of this.slots) {
      if (slot.busy || this.queue.length === 0) continue;
      const job = this.queue.shift();
      if (job.settled) continue;
      slot.busy = true;
      slot.job = job;
      job.slot = slot;
      slot.worker.postMessage({
        config: this.config,
        id: job.id,
        payload: job.payload,
      });
    }
  }

  finish(slot, error, result) {
    const job = slot.job;
    slot.busy = false;
    slot.job = null;
    if (job) {
      job.slot = null;
      this.settle(job, error, result);
    }
    this.drain();
  }

  replace(slot, error) {
    if (slot.replacing) return;
    slot.replacing = true;
    const job = slot.job;
    slot.job = null;
    slot.busy = false;
    this.slots.delete(slot);
    if (job) {
      job.slot = null;
      this.settle(job, error);
    }
    void slot.worker.terminate();
    if (!this.closed) this.spawn();
    this.drain();
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    const error = new Error('Beautify worker pool is closed');
    for (const job of this.queue.splice(0)) this.settle(job, error);
    for (const slot of this.slots) {
      if (slot.job) this.settle(slot.job, error);
      slot.replacing = true;
      void slot.worker.terminate();
    }
    this.slots.clear();
  }
}

async function handleBeautifyRequest(req, res, config, pool) {
  let payload;

  try {
    payload = await readJsonBody(req, bodyLimitForContent(config.maxContentSize));
  } catch (error) {
    writeJson(res, error.statusCode || 500, {
      success: false,
      status: 'failed',
      error: error.message,
    });
    return;
  }

  const { content } = payload;
  const type = typeof payload.type === 'string' ? payload.type : 'unknown';
  const filename = typeof payload.filename === 'string' ? payload.filename : '';
  if (typeof content !== 'string') {
    writeJson(res, 400, {
      success: false,
      status: 'failed',
      error: 'Missing content field',
    });
    return;
  }

  const contentBytes = Buffer.byteLength(content, 'utf8');
  if (contentBytes > config.maxContentSize) {
    writeJson(res, 200, createResult('skipped', content, {
      warning: `Content too large (${contentBytes} bytes), skipped beautification`,
    }));
    return;
  }

  try {
    const result = await pool.submit({ content, filename, type });
    writeJson(res, 200, result);
  } catch (error) {
    writeJson(res, 200, createResult('failed', content, {
      warning: `Beautify failed; original content preserved: ${error.message}`,
    }));
  }
}

function createServer(inputConfig = {}) {
  const config = normalizeConfig(inputConfig);
  const pool = new BeautifyWorkerPool(config);
  const server = http.createServer(async (req, res) => {
    if (req.method === 'GET' && req.url === '/health') {
      writeJson(res, 200, {
        status: 'ok',
        timestamp: Date.now(),
        deobfuscate: config.deobfuscateEnabled,
        pool: pool.stats(),
      });
      return;
    }

    if (req.method === 'POST' && req.url === '/beautify') {
      await handleBeautifyRequest(req, res, config, pool);
      return;
    }

    writeJson(res, 404, {
      success: false,
      status: 'failed',
      error: 'Not found',
    });
  });
  server.on('close', () => pool.close());
  return server;
}

if (!isMainThread) {
  parentPort.on('message', async message => {
    try {
      const { content, filename, type } = message.payload;
      const result = await beautify(content, type, filename, message.config);
      parentPort.postMessage({ id: message.id, result });
    } catch {
      parentPort.postMessage({
        id: message.id,
        result: createResult('failed', message.payload.content, {
          warning: 'Beautify worker failed; original content preserved.',
        }),
      });
    }
  });
}

if (require.main === module && isMainThread) {
  const config = normalizeConfig(getRuntimeConfig());
  const server = createServer(config);

  server.listen(PORT, HOST, () => {
    console.log(`Beautify service running on http://${HOST}:${PORT}`);
    console.log(`Max content size: ${config.maxContentSize} bytes`);
    console.log(`Deobfuscation: ${config.deobfuscateEnabled ? 'enabled' : 'disabled'}`);
    console.log(`Workers: ${config.workerCount}; queue: ${config.queueSize}; timeout: ${config.jobTimeoutMs}ms`);
  });

  const shutdown = signal => {
    console.log(`Received ${signal}, shutting down...`);
    server.close(() => {
      console.log('Server closed');
      process.exit(0);
    });
  };

  process.on('SIGINT', () => shutdown('SIGINT'));
  process.on('SIGTERM', () => shutdown('SIGTERM'));
  process.on('uncaughtException', error => {
    console.error(`Uncaught exception (${error?.constructor?.name || 'unknown'})`);
  });
  process.on('unhandledRejection', reason => {
    console.error(`Unhandled rejection (${reason?.constructor?.name || typeof reason})`);
  });
}

module.exports = {
  createServer,
};
