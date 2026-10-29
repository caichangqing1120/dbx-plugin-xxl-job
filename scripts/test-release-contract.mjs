import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
assert(process.env.DBX_STORE_ROOT, 'Set DBX_STORE_ROOT to a dbx-store checkout to test the real Store contract');
const store = resolve(process.env.DBX_STORE_ROOT);
const manifest = JSON.parse(readFileSync(join(root, 'manifest.json'), 'utf8'));
const listing = JSON.parse(readFileSync(join(root, '.dbx-store.json'), 'utf8'));
const repository = new URL(listing.source).pathname.slice(1);
const tag = `v${manifest.version}`;
const targets = ['darwin-arm64', 'darwin-x64', 'windows-arm64', 'windows-x64', 'linux-arm64', 'linux-x64'];

function workspace(t) {
  const directory = mkdtempSync(join(tmpdir(), 'dbx-release-contract-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  return directory;
}

function run(args, cwd) {
  return spawnSync(process.execPath, args, { cwd, encoding: 'utf8', env: { ...process.env, NODE_OPTIONS: '' } });
}

function succeeds(result) {
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
}

function importAndValidate(t, release, metadata = listing) {
  const directory = workspace(t);
  const output = join(directory, 'candidates', `${manifest.id}.json`);
  writeFileSync(join(directory, 'responses.json'), JSON.stringify({ release, metadata }));
  // Replace only remote JSON retrieval; execute the actual Store importer unchanged.
  const preload = join(directory, 'fetch-fixture.mjs');
  writeFileSync(preload, `
    import { readFileSync } from 'node:fs';
    const fixtures = JSON.parse(readFileSync(${JSON.stringify(join(directory, 'responses.json'))}, 'utf8'));
    globalThis.fetch = async (url) => {
      const fixture = url === 'https://fixture.invalid/release.json' ? fixtures.release
        : url === 'https://fixture.invalid/metadata.json' ? fixtures.metadata : undefined;
      if (fixture === undefined) throw new Error('Unexpected fetch: ' + url);
      return new Response(JSON.stringify(fixture), { status: 200 });
    };
  `);
  const imported = run(['--import', preload, join(store, 'scripts/sync-release-candidate.mjs'),
    '--repository', repository, '--tag', tag,
    '--release-candidates-url', 'https://fixture.invalid/release.json',
    '--metadata-url', 'https://fixture.invalid/metadata.json', '--output', output], directory);
  succeeds(imported);
  const candidate = JSON.parse(readFileSync(output, 'utf8'));
  assert.equal(candidate.id, manifest.id);
  assert.equal(candidate.version, manifest.version);
  assert.equal(candidate.publisher, manifest.publisher);
  assert(!Object.hasOwn(candidate, 'mcpTools'), 'Store candidate must not contain unsupported badge metadata');

  for (const folder of ['scripts', 'plugins', 'publishers', 'catalog']) mkdirSync(join(directory, folder));
  for (const file of ['scripts/validate.mjs', 'signing-keys.json', 'revoked.json', `publishers/${manifest.publisher}.json`]) {
    copyFileSync(join(store, file), join(directory, file));
  }
  succeeds(run([join(directory, 'scripts/validate.mjs'), '--plan-candidates'], directory));
  return candidate;
}

test('package:all emits six artifacts importable by the real Store synchronizer', (t) => {
  const directory = workspace(t);
  copyFileSync(join(root, 'manifest.json'), join(directory, 'manifest.json'));
  copyFileSync(join(root, '.dbx-store.json'), join(directory, '.dbx-store.json'));
  // Keep metadata generation real; substitute only costly compiler/ZIP subprocesses.
  const preload = join(directory, 'build-fixture.mjs');
  writeFileSync(preload, `
    import childProcess from 'node:child_process';
    import { syncBuiltinESMExports } from 'node:module';
    import { createHash } from 'node:crypto';
    import { readFileSync, writeFileSync } from 'node:fs';
    import { join } from 'node:path';
    const manifest = JSON.parse(readFileSync('manifest.json', 'utf8'));
    childProcess.spawnSync = (command, args) => {
      if (args.includes('--target')) {
        const target = args[args.indexOf('--target') + 1];
        const bytes = Buffer.from('public fixture package: ' + target);
        const name = manifest.id + '-' + manifest.version + '-' + target;
        writeFileSync(join('dist', name + '.dbxp'), bytes);
        writeFileSync(join('dist', name + '.artifact.json'), JSON.stringify({
          target, url: name + '.dbxp', sha256: createHash('sha256').update(bytes).digest('hex'), size: bytes.length,
        }));
      } else if (!args.includes('scripts/verify-package.go')) {
        throw new Error('Unexpected subprocess: ' + command + ' ' + args.join(' '));
      }
      return { status: 0 };
    };
    syncBuiltinESMExports();
  `);
  succeeds(run(['--import', preload, join(root, 'scripts/package.mjs'), 'all'], directory));
  const release = JSON.parse(readFileSync(join(directory, 'dist/release-candidates.json'), 'utf8'));
  const candidate = importAndValidate(t, release);
  assert.deepEqual(candidate.targets.map(({ target }) => target).sort(), [...targets].sort());
  assert.deepEqual(candidate.permissions, manifest.permissions ?? []);
  for (const artifact of candidate.targets) {
    const bytes = Buffer.from(`public fixture package: ${artifact.target}`);
    assert.equal(artifact.sha256, createHash('sha256').update(bytes).digest('hex'));
    assert.equal(artifact.size, bytes.length);
    assert.equal(artifact.url, `${listing.source}/releases/download/${tag}/${manifest.id}-${manifest.version}-${artifact.target}.dbxp`);
  }
});

test('active listing is accepted as Store metadata independently of the release builder', (t) => {
  const candidate = importAndValidate(t, {
    plugin: { id: manifest.id, publisher: manifest.publisher, version: manifest.version, name: manifest.name },
    artifacts: [{ target: 'linux-x64', url: 'fixture.dbxp', sha256: 'a'.repeat(64), size: 128 }],
  });
  assert.equal(candidate.name, listing.name);
  assert.equal(candidate.targets[0].url, `${listing.source}/releases/download/${tag}/fixture.dbxp`);
});

test('actual built release imports without reshaping and matches all package bytes', {
  skip: !process.env.DBX_RELEASE_CANDIDATE,
}, (t) => {
  const candidatePath = resolve(process.env.DBX_RELEASE_CANDIDATE);
  const candidate = importAndValidate(t, JSON.parse(readFileSync(candidatePath, 'utf8')));
  assert.deepEqual(candidate.targets.map(({ target }) => target).sort(), [...targets].sort());
  for (const artifact of candidate.targets) {
    const bytes = readFileSync(join(dirname(candidatePath), new URL(artifact.url).pathname.split('/').pop()));
    assert.equal(artifact.sha256, createHash('sha256').update(bytes).digest('hex'));
    assert.equal(artifact.size, bytes.length);
  }
});
