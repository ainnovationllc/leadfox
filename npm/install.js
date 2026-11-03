const crypto = require('node:crypto');
const fs = require('node:fs/promises');
const fsSync = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');

const DEFAULT_BASE_URL = 'https://github.com/ainnovationllc/leadfox/releases';
const PATH_MARKER = '# Leadfox PATH';
const PATH_EXPORT = 'export PATH="{entry}:$PATH"';

function platformKey(platform = process.platform, arch = process.arch) {
  const osName = { darwin: 'darwin', linux: 'linux' }[platform];
  const archName = { arm64: 'arm64', x64: 'amd64' }[arch];
  if (!osName || !archName) {
    throw new Error(`unsupported platform ${platform}/${arch}; supported platforms are darwin/linux arm64/amd64`);
  }
  return { os: osName, arch: archName, key: `${osName}_${archName}` };
}

function normalizeBaseURL(value) {
  const url = new URL(value || DEFAULT_BASE_URL);
  if (url.protocol !== 'https:' && url.hostname !== '127.0.0.1' && url.hostname !== 'localhost') {
    throw new Error(`release base URL must use HTTPS: ${url.href}`);
  }
  return url.href.replace(/\/$/, '');
}

function releaseURL(baseURL, version, file) {
  const base = normalizeBaseURL(baseURL);
  if (version === 'latest') return `${base}/latest/download/${file}`;
  const tag = `v${version.replace(/^v/, '')}`;
  return `${base}/download/${encodeURIComponent(tag)}/${file}`;
}

async function fetchBuffer(url, { maxBytes = 4 * 1024 * 1024 } = {}) {
  const response = await fetch(url, { redirect: 'error' });
  if (!response.ok) throw new Error(`download failed (${response.status}) for ${url}`);
  const length = response.headers.get('content-length');
  if (length && Number(length) > maxBytes) throw new Error(`download exceeds ${maxBytes} bytes: ${url}`);
  const chunks = [];
  let total = 0;
  for await (const chunk of response.body) {
    total += chunk.length;
    if (total > maxBytes) throw new Error(`download exceeds ${maxBytes} bytes: ${url}`);
    chunks.push(chunk);
  }
  return Buffer.concat(chunks);
}

function sha256(value) {
  return crypto.createHash('sha256').update(value).digest('hex');
}

async function sha256File(file) {
  return sha256(await fs.readFile(file));
}

function validateManifest(manifest, platform) {
  if (!manifest || typeof manifest !== 'object' || typeof manifest.version !== 'string') {
    throw new Error('release manifest must contain a version');
  }
  if (!/^[0-9A-Za-z][0-9A-Za-z._+-]*$/.test(manifest.version)) {
    throw new Error(`release manifest has an invalid version ${manifest.version}`);
  }
  if (!Array.isArray(manifest.artifacts)) throw new Error('release manifest must contain artifacts');
  const matches = manifest.artifacts.filter((artifact) => artifact.os === platform.os && artifact.arch === platform.arch);
  if (matches.length !== 1) throw new Error(`release manifest must contain exactly one ${platform.key} artifact`);
  const artifact = matches[0];
  if (!artifact.file || !/^[a-zA-Z0-9._-]+\.tar\.gz$/.test(artifact.file)) throw new Error('manifest artifact has an invalid file name');
  if (!/^[a-f0-9]{64}$/.test(artifact.sha256)) throw new Error(`manifest artifact ${artifact.file} has an invalid SHA-256`);
  return artifact;
}

async function fetchRelease(options, requestedVersion, platform) {
  const baseURL = normalizeBaseURL(options.releaseBaseUrl || process.env.LEADFOX_RELEASE_BASE_URL);
  const manifestURL = releaseURL(baseURL, requestedVersion, 'release-manifest.json');
  let manifest;
  try {
    manifest = JSON.parse((await fetchBuffer(manifestURL)).toString('utf8'));
  } catch (error) {
    throw new Error(`read release manifest: ${error.message}`);
  }
  const artifact = validateManifest(manifest, platform);
  return { baseURL, manifest, artifact, manifestURL, artifactURL: releaseURL(baseURL, manifest.version, artifact.file) };
}

function run(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { ...options, stdio: ['ignore', 'pipe', 'pipe'] });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk; });
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    child.on('error', reject);
    child.on('close', (code) => {
      if (code !== 0) reject(new Error(`${command} ${args.join(' ')} failed (${code}): ${stderr.trim()}`));
      else resolve(stdout);
    });
  });
}

async function validateArchive(archivePath) {
  const listing = await run('tar', ['-tvzf', archivePath]);
  const lines = listing.trim().split('\n').filter(Boolean);
  if (lines.length !== 1 || !/^[-][^ ]*\s+.*\bleadfox$/.test(lines[0])) {
    throw new Error('release archive must contain exactly one regular file named leadfox');
  }
}

function installationRoot(value) {
  return path.resolve(value || path.join(os.homedir(), '.leadfox'));
}

async function ensureWritableRoot(root) {
  const existing = await fs.lstat(root).catch((error) => (error.code === 'ENOENT' ? null : Promise.reject(error)));
  if (existing && (!existing.isDirectory() || existing.isSymbolicLink())) {
    throw new Error(`cannot install: ${root} exists but is not a safe directory`);
  }
  let parent = existing ? root : path.dirname(root);
  while (!existing && !(await fs.lstat(parent).catch(() => null))) {
    const next = path.dirname(parent);
    if (next === parent) break;
    parent = next;
  }
  const probe = await fs.mkdtemp(path.join(parent, '.leadfox-write-test-')).catch((error) => {
    throw new Error(`cannot install: ${parent} is not writable (${error.message})`);
  });
  await fs.rm(probe, { recursive: true, force: true });
}

async function readInstalledManifest(root) {
  const file = path.join(root, 'manifest.json');
  try {
    return JSON.parse(await fs.readFile(file, 'utf8'));
  } catch (error) {
    if (error.code === 'ENOENT') throw new Error(`no Leadfox installation found at ${root}`);
    throw new Error(`read installation manifest: ${error.message}`);
  }
}

async function atomicJSON(file, value, mode) {
  const temporary = `${file}.tmp-${process.pid}-${Date.now()}`;
  await fs.writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`, { mode });
  await fs.chmod(temporary, mode);
  await fs.rename(temporary, file);
}

async function activate(root, target, allowReplace) {
  const binDir = path.join(root, 'bin');
  const active = path.join(binDir, 'leadfox');
  await fs.mkdir(binDir, { recursive: true, mode: 0o700 });
  const existing = await fs.lstat(active).catch((error) => (error.code === 'ENOENT' ? null : Promise.reject(error)));
  if (existing && !existing.isSymbolicLink()) throw new Error(`refusing to replace existing non-Leadfox executable: ${active}`);
  if (existing && (await fs.readlink(active)) === target) return active;
  if (existing && !allowReplace) throw new Error(`Leadfox is already installed at ${active}; use update to replace it`);
  const temporary = `${active}.tmp-${process.pid}-${Date.now()}`;
  await fs.symlink(target, temporary);
  await fs.rename(temporary, active);
  return active;
}

async function install(options = {}) {
  const platform = platformKey(options.platform, options.arch);
  const requestedVersion = options.version || 'latest';
  const root = installationRoot(options.root);
  await ensureWritableRoot(root);
  const release = await fetchRelease(options, requestedVersion, platform);
  const archive = await fetchBuffer(release.artifactURL, { maxBytes: 256 * 1024 * 1024 });
  const archiveHash = sha256(archive);
  if (archiveHash !== release.artifact.sha256) {
    throw new Error(`checksum mismatch for ${release.artifact.file}: expected ${release.artifact.sha256}, got ${archiveHash}`);
  }

  const tempRoot = await fs.mkdtemp(path.join(os.tmpdir(), 'leadfox-install-'));
  try {
    const archivePath = path.join(tempRoot, release.artifact.file);
    const extractRoot = path.join(tempRoot, 'extract');
    await fs.mkdir(extractRoot, { mode: 0o700 });
    await fs.writeFile(archivePath, archive, { mode: 0o600 });
    await validateArchive(archivePath);
    await run('tar', ['-xzf', archivePath, '-C', extractRoot, '--no-same-owner']);
    const extracted = path.join(extractRoot, 'leadfox');
    const stat = await fs.lstat(extracted);
    if (!stat.isFile() || (stat.mode & 0o111) === 0) throw new Error('extracted leadfox is not an executable regular file');

    const releaseDir = path.join(root, 'releases', release.manifest.version, platform.key);
    await fs.mkdir(releaseDir, { recursive: true, mode: 0o700 });
    const target = path.join(releaseDir, 'leadfox');
    try {
      await fs.copyFile(extracted, target, fsSync.constants.COPYFILE_EXCL);
    } catch (error) {
      if (error.code !== 'EEXIST') throw error;
      const existingStat = await fs.lstat(target);
      if (!existingStat.isFile() || await sha256File(target) !== await sha256File(extracted)) {
        throw new Error(`refusing to reuse an existing unverified release file: ${target}`);
      }
    }
    await fs.chmod(target, 0o700);
    const current = await readInstalledManifest(root).catch((error) => {
      if (error.message.startsWith('no Leadfox installation')) return null;
      throw error;
    });
    const active = await activate(root, target, Boolean(options.replaceActive));
    try {
      const binaryHash = await sha256File(target);
      const next = {
        name: 'leadfox',
        version: release.manifest.version,
        platform,
        artifact: release.artifact.file,
        archive_sha256: archiveHash,
        binary_sha256: binaryHash,
        source: release.artifactURL,
        active_binary: path.relative(root, active),
        active_target: path.relative(root, target),
        installed_at: new Date().toISOString(),
      };
      if (current && current.version !== next.version) next.previous = current;
      await atomicJSON(path.join(root, 'manifest.json'), next, 0o600);
      return { installed: true, version: next.version, platform: platform.key, binary: active, archive_sha256: archiveHash, binary_sha256: binaryHash };
    } catch (error) {
      // Activation is provisional until the verified manifest is written.
      // Restore the previous active target so an interrupted update cannot
      // leave a new binary active without a matching manifest.
      if (current && current.active_target) {
        const previousTarget = path.join(root, current.active_target);
        await activate(root, previousTarget, true);
        await atomicJSON(path.join(root, 'manifest.json'), current, 0o600);
      } else {
        await fs.unlink(active).catch(() => {});
        await fs.unlink(path.join(root, 'manifest.json')).catch(() => {});
      }
      throw error;
    }
  } finally {
    await fs.rm(tempRoot, { recursive: true, force: true });
  }
}

async function update(options = {}) {
  const root = installationRoot(options.root);
  const current = await readInstalledManifest(root);
  const platform = platformKey(options.platform, options.arch);
  const release = await fetchRelease(options, options.version || 'latest', platform);
  if (options.check) {
    return { current: current.version, candidate: release.manifest.version, update_available: current.version !== release.manifest.version, platform: platform.key };
  }
  if (current.version === release.manifest.version && !options.force) {
    return { updated: false, reason: 'already-current', version: current.version, platform: platform.key };
  }
  return install({ ...options, root, version: release.manifest.version, replaceActive: true });
}

async function rollback(options = {}) {
  const root = installationRoot(options.root);
  const current = await readInstalledManifest(root);
  if (!current.previous || !current.previous.active_target) throw new Error('no previous verified Leadfox version is available for rollback');
  const target = path.join(root, current.previous.active_target);
  const targetStat = await fs.lstat(target).catch(() => null);
  if (!targetStat || !targetStat.isFile() || await sha256File(target) !== current.previous.binary_sha256) {
    throw new Error('previous Leadfox version is missing or failed its recorded checksum');
  }
  const active = await activate(root, target, true);
  const next = { ...current.previous, previous: current, active_binary: path.relative(root, active), active_target: path.relative(root, target), rolled_back_at: new Date().toISOString() };
  await atomicJSON(path.join(root, 'manifest.json'), next, 0o600);
  return { rolled_back: true, version: next.version, binary: active };
}

function shellConfigPath(options = {}) {
  if (options.file) return path.resolve(options.file);
  if (process.platform === 'win32') throw new Error('automatic PATH editing for Windows is not implemented');
  const shell = path.basename(process.env.SHELL || '');
  if (shell === 'zsh') return path.join(os.homedir(), '.zshrc');
  if (shell === 'bash') return path.join(os.homedir(), '.bashrc');
  throw new Error(`unsupported shell ${shell || '(unknown)'}; use --file to select a shell configuration file`);
}

function pathEntry(root) {
  const homeRoot = path.join(os.homedir(), '.leadfox');
  return root === homeRoot ? '$HOME/.leadfox/bin' : path.join(root, 'bin');
}

async function addPath(options = {}) {
  const root = installationRoot(options.root);
  const file = shellConfigPath(options);
  const entry = pathEntry(root);
  const exportLine = PATH_EXPORT.replace('{entry}', entry);
  let content = await fs.readFile(file, 'utf8').catch((error) => (error.code === 'ENOENT' ? '' : Promise.reject(error)));
  if (content.includes(entry)) return { changed: false, file, entry };
  if (content.includes(PATH_MARKER)) {
    if (content.includes(exportLine)) return { changed: false, file, entry };
    throw new Error(`a Leadfox PATH marker already exists in ${file} with a different entry`);
  }
  const addition = `${content && !content.endsWith('\n') ? '\n' : ''}${PATH_MARKER}\n${exportLine}\n`;
  await fs.writeFile(file, content + addition, { mode: 0o600 });
  return { changed: true, file, entry };
}

async function removePath(options = {}) {
  const root = installationRoot(options.root);
  const file = shellConfigPath(options);
  const entry = pathEntry(root);
  const content = await fs.readFile(file, 'utf8').catch((error) => (error.code === 'ENOENT' ? '' : Promise.reject(error)));
  const lines = content.split('\n');
  const filtered = [];
  for (let i = 0; i < lines.length; i += 1) {
    if (lines[i] === PATH_MARKER && lines[i + 1] === PATH_EXPORT.replace('{entry}', entry)) { i += 1; continue; }
    filtered.push(lines[i]);
  }
  const next = filtered.join('\n');
  if (next !== content) await fs.writeFile(file, next, { mode: 0o600 });
  return { changed: next !== content, file, entry };
}

async function doctor(options = {}) {
  const root = installationRoot(options.root);
  const findings = [];
  let manifest;
  try { manifest = await readInstalledManifest(root); } catch (error) { return { healthy: false, root, findings: [error.message] }; }
  const target = path.join(root, manifest.active_target || '');
  const active = path.join(root, manifest.active_binary || 'bin/leadfox');
  const activeLink = await fs.readlink(active).catch(() => null);
  if (!activeLink || path.resolve(path.dirname(active), activeLink) !== target) findings.push('active binary symlink does not match the installation manifest');
  const targetStat = await fs.lstat(target).catch(() => null);
  if (!targetStat || !targetStat.isFile()) findings.push('active binary is missing or not a regular file');
  else if (!manifest.binary_sha256) findings.push('installation manifest has no active binary checksum');
  else if (await sha256File(target) !== manifest.binary_sha256) findings.push('active binary checksum does not match the installation manifest');
  const expectedPath = path.join(root, 'bin');
  const pathPresent = (process.env.PATH || '').split(path.delimiter).includes(expectedPath);
  return { healthy: findings.length === 0, root, version: manifest.version, platform: manifest.platform?.key, binary: active, path_present: pathPresent, findings };
}

module.exports = { addPath, doctor, ensureWritableRoot, install, platformKey, removePath, rollback, sha256, update, validateManifest };
