#!/bin/zsh
set -euo pipefail
python3 - "${0:A:h}" <<'PYTHON'
import json
import os
from pathlib import Path
import plistlib
import shlex
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

root = Path(sys.argv[1])
base = Path.home() / '.new-api-local'
config = {}
for line in (root / 'local.env').read_text().splitlines():
    if not line.strip() or line.lstrip().startswith('#'):
        continue
    key, value = line.split('=', 1)
    parts = shlex.split(value, comments=True)
    if len(parts) != 1:
        raise SystemExit('local.env must contain one literal value per variable')
    config[key.strip()] = parts[0]
for key in ('SQL_DSN', 'LOG_SQL_DSN'):
    value = config.get(key, '')
    if key == 'LOG_SQL_DSN' and not value:
        continue
    if urllib.parse.urlsplit(value).scheme not in ('postgres', 'postgresql'):
        raise SystemExit(key + ' must use PostgreSQL')
config.pop('SQLITE_PATH', None)
if not (root / 'new-api').is_file():
    raise SystemExit('Build the local new-api executable before starting')

launch_dir = Path.home() / 'Library/LaunchAgents'
domain = 'gui/' + str(os.getuid())
pg_label = 'com.newapi.postgres.local'
pg_plist = launch_dir / (pg_label + '.plist')
dsn = urllib.parse.urlsplit(config['SQL_DSN'])
if dsn.hostname in ('localhost', '127.0.0.1') and (dsn.port or 5432) == 5432 and pg_plist.exists():
    loaded = subprocess.run(['launchctl', 'print', domain + '/' + pg_label], capture_output=True)
    if loaded.returncode:
        subprocess.run(['launchctl', 'bootstrap', domain, str(pg_plist)], check=True)
    pg_config = plistlib.loads(pg_plist.read_bytes())
    pg_ready = Path(pg_config['ProgramArguments'][0]).with_name('pg_isready')
    for _ in range(30):
        result = subprocess.run([str(pg_ready), '-h', dsn.hostname, '-p', str(dsn.port or 5432),
                                 '-U', dsn.username or '', '-d', dsn.path.lstrip('/')], capture_output=True)
        if result.returncode == 0:
            break
        time.sleep(0.5)
    else:
        raise SystemExit('Local PostgreSQL did not become ready')

label = 'com.newapi.local'
plist = launch_dir / (label + '.plist')
(base / 'logs').mkdir(parents=True, exist_ok=True)
subprocess.run(['launchctl', 'bootout', domain + '/' + label], capture_output=True)
shutil.copy2(root / 'new-api', base / 'new-api')
(base / 'new-api').chmod(0o755)
content = {
    'Label': label,
    'ProgramArguments': [str(base / 'new-api'), '--log-dir', str(base / 'logs')],
    'WorkingDirectory': str(base),
    'EnvironmentVariables': config,
    'RunAtLoad': True,
    'KeepAlive': True,
    'StandardOutPath': '/tmp/new-api.launchd.stdout.log',
    'StandardErrorPath': '/tmp/new-api.launchd.stderr.log',
}
launch_dir.mkdir(parents=True, exist_ok=True)
fd = os.open(plist, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
with os.fdopen(fd, 'wb') as output:
    output.write(plistlib.dumps(content))
plist.chmod(0o600)
subprocess.run(['launchctl', 'bootstrap', domain, str(plist)], check=True)
url = 'http://127.0.0.1:' + config.get('PORT', '3000')
for _ in range(60):
    try:
        with urllib.request.urlopen(url + '/api/status', timeout=2) as response:
            if json.load(response).get('success'):
                print('New API ready with PostgreSQL: ' + url + '/')
                break
    except (OSError, ValueError):
        pass
    time.sleep(0.5)
else:
    raise SystemExit('New API did not become ready; check /tmp/new-api.launchd.stderr.log and stdout.log')
PYTHON
