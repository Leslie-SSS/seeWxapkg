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
    assert.equal(payload.content, content);
    assert.match(payload.warning, /Content too large/);
  } finally {
    await close(server);
  }
});
