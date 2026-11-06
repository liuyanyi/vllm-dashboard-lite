"""Archive one prebuilt target; no downloads or extra builds."""
import argparse
import hashlib
from pathlib import Path
import shutil
import tarfile
import zipfile

parser = argparse.ArgumentParser()
parser.add_argument('--edition', choices=['web', 'desktop'], required=True)
parser.add_argument('--platform', choices=['windows', 'linux'], required=True)
args = parser.parse_args()
root = Path(__file__).resolve().parents[1]
source = root / ('release' if args.edition == 'web' else 'desktop/release/' + ('win-unpacked' if args.platform == 'windows' else 'linux-unpacked'))
if not source.is_dir() or not any(source.iterdir()):
    raise SystemExit(f'Missing build directory: {source}')
shutil.copy2(root / 'README.md', source / 'README.md')
out = root / 'artifacts'
out.mkdir(exist_ok=True)
name = f'vllm-dashboard-lite-{args.edition}-{args.platform}-x64'
if args.platform == 'windows':
    archive = out / (name + '.zip')
    with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=1) as target:
        for path in sorted(source.rglob('*')):
            if path.is_file():
                target.write(path, Path(name) / path.relative_to(source))
else:
    archive = out / (name + '.tar.gz')
    with tarfile.open(archive, 'w:gz', compresslevel=1) as target:
        target.add(source, arcname=name)
digest = hashlib.sha256()
with archive.open('rb') as stream:
    for chunk in iter(lambda: stream.read(1024 * 1024), b''):
        digest.update(chunk)
(out / (archive.name + '.sha256')).write_text(digest.hexdigest() + '  ' + archive.name + '\n')
print(archive.name)
