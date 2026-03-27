/**
 * HTTP wrapper around the beautify core.
 */

const http = require('http');

const { beautify, getRuntimeConfig } = require('./core');

const PORT = parseInt(process.env.BEAUTIFY_PORT || '3001', 10);
const HOST = process.env.BEAUTIFY_HOST || '127.0.0.1';

function writeJson(res, statusCode, payload) {
  res.writeHead(statusCode, {
    'Content-Type': 'application/json',
  });
  res.end(JSON.stringify(payload));
}

function readJsonBody(req, maxContentSize) {
  return new Promise((resolve, reject) => {
    let body = '';

    req.on('data', chunk => {
      body += chunk;
      if (body.length > maxContentSize + 10000) {
        const error = new Error('Content too large');
        error.statusCode = 413;
        req.destroy(error);
      }
    });

    req.on('end', () => {
      try {
        resolve(JSON.parse(body));
      } catch (error) {
        error.statusCode = 400;
        reject(error);
      }
    });

    req.on('error', reject);
  });
}

async function handleBeautifyRequest(req, res, config) {
  let payload;

  try {
    payload = await readJsonBody(req, config.maxContentSize);
  } catch (error) {
    const statusCode = error.statusCode || 500;
    writeJson(res, statusCode, {
      success: false,
      error: error.message,
    });
    return;
  }

  const { content, type, filename } = payload;
  if (typeof content !== 'string') {
    writeJson(res, 400, {
      success: false,
      error: 'Missing content field',
    });
    return;
  }

  if (content.length > config.maxContentSize) {
    writeJson(res, 200, {
      success: true,
      content,
      warning: 'Content too large, skipped beautification',
    });
    return;
  }

  try {
    const result = await beautify(content, type, filename, config);
    writeJson(res, 200, result);
  } catch (error) {
    writeJson(res, 200, {
      success: true,
      content,
      warning: `Beautify failed, returning original content: ${error.message}`,
    });
  }
}

function createServer(config = getRuntimeConfig()) {
  return http.createServer(async (req, res) => {
    if (req.method === 'GET' && req.url === '/health') {
      writeJson(res, 200, {
        status: 'ok',
        timestamp: Date.now(),
        deobfuscate: config.deobfuscateEnabled,
      });
      return;
    }

    if (req.method === 'POST' && req.url === '/beautify') {
      await handleBeautifyRequest(req, res, config);
      return;
    }

    writeJson(res, 404, {
      success: false,
      error: 'Not found',
    });
  });
}

if (require.main === module) {
  const config = getRuntimeConfig();
  const server = createServer(config);

  server.listen(PORT, HOST, () => {
    console.log(`Beautify service running on http://${HOST}:${PORT}`);
    console.log(`Max content size: ${config.maxContentSize} bytes`);
    console.log(`Deobfuscation: ${config.deobfuscateEnabled ? 'enabled' : 'disabled'}`);
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
    console.error('Uncaught exception:', error);
  });
  process.on('unhandledRejection', reason => {
    console.error('Unhandled rejection:', reason);
  });
}

module.exports = {
  createServer,
  handleBeautifyRequest,
  readJsonBody,
};
