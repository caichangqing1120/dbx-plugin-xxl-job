import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { basename, join } from 'node:path';

const platform = process.platform === 'win32' ? 'windows' : process.platform;
const hostTarget = `${platform}-${process.arch}`;
const targets = ['darwin-arm64', 'darwin-x64', 'windows-arm64', 'windows-x64', 'linux-arm64', 'linux-x64'];
const requestedTarget = process.argv[2] || `${platform}-${process.arch}`;
const cli = process.env.DBX_PLUGIN_CLI || 'dbx-plugin';
if (requestedTarget !== 'all' && !targets.includes(requestedTarget)) {
  throw new Error(`Unsupported package target: ${requestedTarget}`);
}

const manifest = JSON.parse(readFileSync('manifest.json', 'utf8'));
const selectedTargets = requestedTarget === 'all' ? targets : [requestedTarget];
const artifactPaths = selectedTargets.map((target) => join('dist', `${manifest.id}-${manifest.version}-${target}.dbxp`));

mkdirSync('dist', { recursive: true });
if (requestedTarget === 'all') {
  for (const target of targets) {
    const prefix = join('dist', `${manifest.id}-${manifest.version}-${target}`);
    rmSync(`${prefix}.dbxp`, { force: true });
    rmSync(`${prefix}.artifact.json`, { force: true });
  }
  rmSync(join('dist', `SHA256SUMS-v${manifest.version}.txt`), { force: true });
  rmSync(join('dist', 'release-candidates.json'), { force: true });
}

for (const target of selectedTargets) {
  const [os, arch] = target.split('-');
  const env = { ...process.env };
  if (target === hostTarget) {
    env.GOOS = os;
    env.GOARCH = arch === 'x64' ? 'amd64' : arch;
    env.CGO_ENABLED = '0';
  } else {
    delete env.GOOS;
    delete env.GOARCH;
    delete env.CGO_ENABLED;
  }
  delete env.DBX_PLUGIN_SIGNING_KEY;
  delete env.DBX_PLUGIN_SIGNING_PUBLIC_KEY;
  const command = target === hostTarget ? cli : 'go';
  const args = target === hostTarget
    ? ['package', '.', '--target', target, '--output-dir', 'dist']
    : ['run', 'scripts/package-cross.go', '--target', target, '--output-dir', 'dist'];
  const result = spawnSync(command, args, { stdio: 'inherit', shell: false, env });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
  const verify = spawnSync('go', ['run', 'scripts/verify-package.go', join('dist', `${manifest.id}-${manifest.version}-${target}.dbxp`)], { stdio: 'inherit', shell: false });
  if (verify.error) throw verify.error;
  if (verify.status !== 0) process.exit(verify.status ?? 1);
}

if (requestedTarget === 'all') {
  const checksums = artifactPaths.map((artifactPath) => {
    const digest = createHash('sha256').update(readFileSync(artifactPath)).digest('hex');
    return `${digest}  ${basename(artifactPath)}`;
  });
  const checksumPath = join('dist', `SHA256SUMS-v${manifest.version}.txt`);
  writeFileSync(checksumPath, `${checksums.join('\n')}\n`);
  console.log(`Wrote ${checksumPath} for ${artifactPaths.length} packages`);

  const listing = JSON.parse(readFileSync('.dbx-store.json', 'utf8'));
  const tag = `v${manifest.version}`;
  const sourceUrl = new URL(listing.source);
  const repository = sourceUrl.pathname.split('/').filter(Boolean).slice(0, 2).join('/');
  if (sourceUrl.protocol !== 'https:' || sourceUrl.hostname !== 'github.com' || repository.split('/').length !== 2) {
    throw new Error('Store source must reference a public GitHub repository or tag');
  }
  const repositoryUrl = `https://github.com/${repository}`;
  const candidate = {
    plugin: {
      id: manifest.id,
      publisher: manifest.publisher,
      version: manifest.version,
      name: manifest.name,
      description: manifest.description,
      permissions: manifest.permissions ?? [],
    },
    artifacts: targets.map((target, index) => {
      const artifact = artifactPaths[index];
      const metadata = JSON.parse(readFileSync(artifact.replace(/\.dbxp$/, '.artifact.json'), 'utf8'));
      const sha256 = createHash('sha256').update(readFileSync(artifact)).digest('hex');
      if (metadata.target !== target || metadata.sha256 !== sha256 || metadata.size !== readFileSync(artifact).length) {
        throw new Error(`Candidate metadata mismatch: ${target}`);
      }
      return { target, url: `${repositoryUrl}/releases/download/${tag}/${basename(artifact)}`, sha256, size: metadata.size };
    }),
  };
  writeFileSync(join('dist', 'release-candidates.json'), `${JSON.stringify(candidate, null, 2)}\n`);
  console.log('Wrote dist/release-candidates.json');
}
