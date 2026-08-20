const assert = require('node:assert/strict');
const test = require('node:test');

const { createServer } = require('./server');

function listen(server) {
  return new Promise(resolve => {
    server.listen(0, '127.0.0.1', () => {
      resolve(server.address());
    });
  });
}

function close(server) {
  return new Promise((resolve, reject) => {
    server.close(error => {
      if (error) {
        reject(error);
        return;
      }
      resolve();
    });
  });
}

test('server returns 400 for invalid JSON payloads', async () => {
  const server = createServer({
    deobfuscateEnabled: true,
    maxContentSize: 1024,
  });
  const address = await listen(server);

  try {
    const response = await fetch(`http://${address.address}:${address.port}/beautify`, {
      body: '{bad json}',
      headers: {
        'Content-Type': 'application/json',
      },
      method: 'POST',
    });

    assert.equal(response.status, 400);
    const payload = await response.json();
    assert.equal(payload.success, false);
    assert.match(payload.error, /Expected property name|Unexpected token/);
  } finally {
    await close(server);
  }
});

test('server returns original content for oversized payloads', async () => {
  const server = createServer({
    deobfuscateEnabled: true,
    maxContentSize: 7,
  });
  const address = await listen(server);
  const content = 'Page({})';

  try {
    const response = await fetch(`http://${address.address}:${address.port}/beautify`, {
      body: JSON.stringify({
        content,
        filename: 'index.js',
        type: 'javascript',
      }),
      headers: {
        'Content-Type': 'application/json',
      },
      method: 'POST',
    });

    assert.equal(response.status, 200);
    const payload = await response.json();
    assert.equal(payload.success, true);
    assert.equal(payload.status, 'skipped');
    assert.equal(payload.content, content);
    assert.match(payload.warning, /Content too large/);
  } finally {
    await close(server);
  }
});

test('server skips large JavaScript before it enters a worker', async () => {
  const server = createServer({
    deobfuscateEnabled: false,
    formatMaxContentSize: 16,
    maxContentSize: 1024,
    workerCount: 1,
  });
  const address = await listen(server);
  const content = 'const value = ' + '1'.repeat(32);

  try {
    const response = await fetch(`http://${address.address}:${address.port}/beautify`, {
      body: JSON.stringify({ content, filename: 'common/game.js', type: 'javascript' }),
      headers: { 'Content-Type': 'application/json' },
      method: 'POST',
    });
    assert.equal(response.status, 200);
    const payload = await response.json();
    assert.equal(payload.success, true);
    assert.equal(payload.status, 'skipped');
    assert.equal(payload.content, content);
    assert.match(payload.warning, /safe formatter/);
  } finally {
    await close(server);
  }
});

test('server enforces the content limit in UTF-8 bytes', async () => {
  const server = createServer({
    deobfuscateEnabled: false,
    jobTimeoutMs: 5000,
    maxContentSize: 4,
    queueSize: 4,
    workerCount: 1,
  });
  const address = await listen(server);
  const content = '你好';

  try {
    const response = await fetch(`http://${address.address}:${address.port}/beautify`, {
      body: JSON.stringify({ content, filename: 'index.js', type: 'javascript' }),
      headers: { 'Content-Type': 'application/json' },
      method: 'POST',
    });
    assert.equal(response.status, 200);
    const payload = await response.json();
    assert.equal(payload.status, 'skipped');
    assert.match(payload.warning, /6 bytes/);
  } finally {
    await close(server);
  }
});

test('server accepts worst-case JSON escaping for content within the byte limit', async () => {
  const server = createServer({
    deobfuscateEnabled: false,
    jobTimeoutMs: 5000,
    maxContentSize: 8,
    queueSize: 4,
    workerCount: 1,
  });
  const address = await listen(server);
  const content = '\u0001\u0002\u0003\u0004';

  try {
    const response = await fetch(`http://${address.address}:${address.port}/beautify`, {
      body: JSON.stringify({ content, filename: 'data.bin', type: 'unknown' }),
      headers: { 'Content-Type': 'application/json' },
      method: 'POST',
    });
    assert.equal(response.status, 200);
    const payload = await response.json();
    assert.equal(payload.content, content);
    assert.equal(payload.status, 'skipped');
  } finally {
    await close(server);
  }
});

test('server returns a clean 413 when the JSON envelope exceeds its hard limit', async () => {
  const server = createServer({
    deobfuscateEnabled: false,
    jobTimeoutMs: 5000,
    maxContentSize: 8,
    queueSize: 4,
    workerCount: 1,
  });
  const address = await listen(server);

  try {
    const response = await fetch(`http://${address.address}:${address.port}/beautify`, {
      body: JSON.stringify({ content: 'x', padding: 'x'.repeat(70000), type: 'unknown' }),
      headers: { 'Content-Type': 'application/json' },
      method: 'POST',
    });
    assert.equal(response.status, 413);
    const payload = await response.json();
    assert.equal(payload.success, false);
    assert.equal(payload.status, 'failed');
  } finally {
    await close(server);
  }
});

test('worker deadline returns original content and terminates timed-out work', async () => {
  const server = createServer({
    deobfuscateEnabled: false,
    jobTimeoutMs: 1,
    maxContentSize: 1024,
    queueSize: 1,
    workerCount: 1,
  });
  const address = await listen(server);
  const content = 'const value=1';

  try {
    const response = await fetch(`http://${address.address}:${address.port}/beautify`, {
      body: JSON.stringify({ content, filename: 'index.js', type: 'javascript' }),
      headers: { 'Content-Type': 'application/json' },
      method: 'POST',
    });
    assert.equal(response.status, 200);
    const payload = await response.json();
    assert.equal(payload.status, 'failed');
    assert.equal(payload.content, content);
    assert.match(payload.warning, /timed out/);
  } finally {
    await close(server);
  }
});

test('bounded worker pool handles concurrent formatting requests', async () => {
  const server = createServer({
    deobfuscateEnabled: false,
    jobTimeoutMs: 5000,
    maxContentSize: 128000,
    queueSize: 16,
    workerCount: 2,
  });
  const address = await listen(server);
  const content = `const values=[${Array.from({ length: 2000 }, (_, index) => index).join(',')}];`;

  try {
    const requests = Array.from({ length: 12 }, (_, index) =>
      fetch(`http://${address.address}:${address.port}/beautify`, {
        body: JSON.stringify({ content, filename: `file-${index}.js`, type: 'javascript' }),
        headers: { 'Content-Type': 'application/json' },
        method: 'POST',
      }).then(async response => ({ response, payload: await response.json() }))
    );
    const results = await Promise.all(requests);
    for (const { response, payload } of results) {
      assert.equal(response.status, 200);
      assert.equal(payload.success, true);
      assert.equal(payload.status, 'formatted');
    }
  } finally {
    await close(server);
  }
});
