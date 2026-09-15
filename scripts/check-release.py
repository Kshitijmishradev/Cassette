"""Reject unexpected release payloads; run after a GoReleaser snapshot build."""
import hashlib
from pathlib import Path
import sys
import tarfile

root = Path(sys.argv[1])
archives = sorted(root.glob('cassette_*.tar.gz'))
expected_platforms = {'darwin_amd64', 'darwin_arm64', 'linux_amd64', 'linux_arm64'}
assert len(archives) == 4, f'expected four archives, found {len(archives)}'
checksums = {}
for line in (root / 'checksums.txt').read_text().splitlines():
    digest, name = line.split(None, 1)
    checksums[name.strip().lstrip('*')] = digest
for archive in archives:
    platform = '_'.join(archive.name.removesuffix('.tar.gz').split('_')[-2:])
    assert platform in expected_platforms, f'unexpected platform: {platform}'
    expected_platforms.remove(platform)
    assert hashlib.sha256(archive.read_bytes()).hexdigest() == checksums[archive.name]
    with tarfile.open(archive) as package:
        members = package.getmembers()
        assert len(members) == 4
        assert {m.name for m in members} == {'cassette', 'LICENSE', 'QUICKSTART.md', 'SECURITY.md'}
        assert all(m.isfile() for m in members), 'links and directories are forbidden'
        assert package.getmember('cassette').mode & 0o111, 'binary is not executable'
    print(f'{archive.name}: contents and SHA-256 verified')
assert not expected_platforms
