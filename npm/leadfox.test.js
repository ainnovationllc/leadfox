const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const http = require('node:http');
const { spawn } = require('node:child_process');
const { addPath, doctor, install, platformKey, removePath, rollback, update, validateManifest, sha256 } = require('./install.js');

test('maps supported platforms to release keys', () => {
  assert.deepEqual(platformKey('darwin', 'arm64'), { os: 'darwin', arch: 'arm64', key: 'darwin_arm64' });
  assert.deepEqual(platformKey('linux', 'x64'), { os: 'linux', arch: 'amd64', key: 'linux_amd64' });
});

test('rejects unsupported platforms', () => {
  assert.throws(() => platformKey('win32', 'x64'), /unsupported platform/);
});

test('selects exactly one matching manifest artifact', () => {
  const artifact = validateManifest({
    version: '0.1.0',
    artifacts: [
      { os: 'linux', arch: 'amd64', file: 'leadfox_0.1.0_linux_amd64.tar.gz', sha256: 'a'.repeat(64) },
      { os: 'darwin', arch: 'arm64', file: 'leadfox_0.1.0_darwin_arm64.tar.gz', sha256: 'b'.repeat(64) },
    ],
  }, { os: 'linux', arch: 'amd64' });
  assert.equal(artifact.file, 'leadfox_0.1.0_linux_amd64.tar.gz');
});

test('rejects duplicate platform artifacts', () => {
  const item = { os: 'linux', arch: 'amd64', file: 'leadfox_0.1.0_linux_amd64.tar.gz', sha256: 'a'.repeat(64) };
  assert.throws(() => validateManifest({ version: '0.1.0', artifacts: [item, item] }, { os: 'linux', arch: 'amd64' }), /exactly one/);
});

test('computes a stable SHA-256', () => {
  assert.equal(sha256(Buffer.from('leadfox')), 'a0a90fe45aa590260e9ffdcc172088ffdb43e386e2d9f10cdf16a705bebb4b71');
});

function command(name, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(name, args, { ...options, stdio: ['ignore', 'pipe', 'pipe'] });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk; });
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    child.on('error', reject);
    child.on('close', (code) => code === 0 ? resolve(stdout) : reject(new Error(stderr)));
  });
}

test('installs a verified local release through the same download path', async () => {
  const temp = await fs.mkdtemp(path.join(os.tmpdir(), 'leadfox-bootstrap-test-'));
  const source = path.join(temp, 'source');
  const release = path.join(temp, 'release');
  const root = path.join(temp, 'home', '.leadfox');
  await fs.mkdir(source, { recursive: true });
  await fs.mkdir(release, { recursive: true });
  const executable = path.join(source, 'leadfox');
  await fs.writeFile(executable, '#!/bin/sh\nprintf verified\n', { mode: 0o700 });
  const archive = path.join(release, 'leadfox_0.1.0_linux_amd64.tar.gz');
  await command('tar', ['-czf', archive, '-C', source, 'leadfox']);
  const archiveBytes = await fs.readFile(archive);
  const source2 = path.join(temp, 'source2');
  await fs.mkdir(source2, { recursive: true });
  await fs.writeFile(path.join(source2, 'leadfox'), '#!/bin/sh\nprintf updated\n', { mode: 0o700 });
  const archive2 = path.join(release, 'leadfox_0.2.0_linux_amd64.tar.gz');
  await command('tar', ['-czf', archive2, '-C', source2, 'leadfox']);
  const archiveBytes2 = await fs.readFile(archive2);
  const manifest1 = JSON.stringify({
    version: '0.1.0',
    artifacts: [{ os: 'linux', arch: 'amd64', file: path.basename(archive), sha256: sha256(archiveBytes) }],
  });
  const manifest2 = JSON.stringify({
    version: '0.2.0',
    artifacts: [{ os: 'linux', arch: 'amd64', file: path.basename(archive2), sha256: sha256(archiveBytes2) }],
  });
  let manifest = manifest1;
  const server = http.createServer(async (request, response) => {
    const files = {
      '/releases/download/latest/release-manifest.json': () => Buffer.from(manifest),
      '/releases/download/v0.1.0/release-manifest.json': () => Buffer.from(manifest1),
      '/releases/download/v0.2.0/release-manifest.json': () => Buffer.from(manifest2),
      '/releases/download/v0.1.0/leadfox_0.1.0_linux_amd64.tar.gz': archiveBytes,
      '/releases/download/v0.2.0/leadfox_0.2.0_linux_amd64.tar.gz': archiveBytes2,
    };
    const body = typeof files[request.url] === 'function' ? files[request.url]() : files[request.url];
    if (!body) { response.writeHead(404); response.end('not found'); return; }
    response.writeHead(200, { 'content-length': body.length });
    response.end(body);
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  try {
    const address = server.address();
    const result = await install({ root, platform: 'linux', arch: 'x64', releaseBaseUrl: `http://127.0.0.1:${address.port}/releases` });
    assert.equal(result.version, '0.1.0');
    assert.equal(result.platform, 'linux_amd64');
    assert.equal((await fs.readlink(path.join(root, 'bin', 'leadfox'))).endsWith('/leadfox'), true);
    const installedManifest = JSON.parse(await fs.readFile(path.join(root, 'manifest.json'), 'utf8'));
    assert.equal(installedManifest.archive_sha256, sha256(archiveBytes));
    const diagnosis = await doctor({ root });
    assert.equal(diagnosis.healthy, true);
    const shellFile = path.join(temp, '.zshrc');
    assert.equal((await addPath({ root, file: shellFile })).changed, true);
    assert.equal((await addPath({ root, file: shellFile })).changed, false);
    assert.match(await fs.readFile(shellFile, 'utf8'), /Leadfox PATH/);
    assert.equal((await removePath({ root, file: shellFile })).changed, true);
    assert.doesNotMatch(await fs.readFile(shellFile, 'utf8'), /Leadfox PATH/);
    manifest = manifest2;
    const updated = await update({ root, platform: 'linux', arch: 'x64', yes: true, releaseBaseUrl: `http://127.0.0.1:${address.port}/releases` });
    assert.equal(updated.version, '0.2.0');
    assert.equal((await doctor({ root })).version, '0.2.0');
    const restored = await rollback({ root });
    assert.equal(restored.version, '0.1.0');
    assert.equal((await doctor({ root })).version, '0.1.0');
  } finally {
    await new Promise((resolve) => server.close(resolve));
    await fs.rm(temp, { recursive: true, force: true });
  }
});

test('rejects a checksum mismatch before creating an installation', async () => {
  const temp = await fs.mkdtemp(path.join(os.tmpdir(), 'leadfox-checksum-test-'));
  const source = path.join(temp, 'source');
  const release = path.join(temp, 'release');
  const root = path.join(temp, 'home', '.leadfox');
  await fs.mkdir(source, { recursive: true });
  await fs.mkdir(release, { recursive: true });
  const archive = path.join(release, 'leadfox_0.1.0_linux_amd64.tar.gz');
  await fs.writeFile(path.join(source, 'leadfox'), '#!/bin/sh\nprintf verified\n', { mode: 0o700 });
  await command('tar', ['-czf', archive, '-C', source, 'leadfox']);
  const archiveBytes = await fs.readFile(archive);
  const manifest = JSON.stringify({
    version: '0.1.0',
    artifacts: [{ os: 'linux', arch: 'amd64', file: path.basename(archive), sha256: '0'.repeat(64) }],
  });
  const server = http.createServer((request, response) => {
    const body = request.url.endsWith('release-manifest.json') ? Buffer.from(manifest) : archiveBytes;
    response.writeHead(200, { 'content-length': body.length });
    response.end(body);
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  try {
    const address = server.address();
    await assert.rejects(
      install({ root, platform: 'linux', arch: 'x64', releaseBaseUrl: `http://127.0.0.1:${address.port}/releases` }),
      /checksum mismatch/,
    );
    await assert.rejects(fs.stat(root), { code: 'ENOENT' });
  } finally {
    await new Promise((resolve) => server.close(resolve));
    await fs.rm(temp, { recursive: true, force: true });
  }
});
