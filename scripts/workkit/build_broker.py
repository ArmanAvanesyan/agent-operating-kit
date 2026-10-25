#!/usr/bin/env python3
"""Explicit native broker build. Never starts the broker or opens native stores."""
import argparse
import hashlib
import json
import os
import pathlib
import platform
import subprocess

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--out',required=True)
parser.add_argument('--go',required=True)
parser.add_argument('--swiftc')
parser.add_argument('--sdk')
args=parser.parse_args()
root=pathlib.Path(__file__).resolve().parents[2]

def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def snapshot():
    files=[]
    for name in ('kits/workkit','integrations/legacy-aok/workkit'):
        files.extend(p for p in (root/name).rglob('*') if p.is_file() and p.suffix in ('.go','.swift','.json') and 'testdata' not in p.parts and not p.name.endswith('_test.go'))
    files.extend([root/'go.mod',root/'go.sum',pathlib.Path(__file__).resolve()])
    return [{'path':p.relative_to(root).as_posix(),'sha256':sha(p)} for p in sorted(set(files))]

before=snapshot()
out=pathlib.Path(args.out).absolute()
out.mkdir(mode=0o700,parents=False,exist_ok=False)
compiler=pathlib.Path(args.go).absolute()
if not pathlib.Path(args.go).is_absolute(): raise SystemExit('absolute pinned Go compiler required')
env=os.environ.copy()
env.update(GOTOOLCHAIN='local',CGO_ENABLED='0',GOOS={'Darwin':'darwin','Linux':'linux','Windows':'windows'}[platform.system()],GOARCH={'x86_64':'amd64','AMD64':'amd64','arm64':'arm64','aarch64':'arm64'}[platform.machine()])
commands=[[str(compiler),'build','-o',str(out/('workkit-secret-broker.exe' if platform.system()=='Windows' else 'workkit-secret-broker')),'./kits/workkit/cmd/workkit-secret-broker']]
sdk=None
if platform.system()=='Darwin':
    if not args.swiftc or not args.sdk or not pathlib.Path(args.swiftc).is_absolute() or not pathlib.Path(args.sdk).is_absolute():
        raise SystemExit('absolute pinned Swift compiler and --sdk required on macOS')
    # Preserve swiftc argv[0]: resolving its multicall symlink changes driver mode.
    swift=pathlib.Path(args.swiftc).absolute()
    sdk_path=pathlib.Path(args.sdk).resolve()
    settings=sdk_path/'SDKSettings.json'
    sdk={'path':str(sdk_path),'settingsSha256':sha(settings)}
    commands.append([str(swift),'-sdk',str(sdk_path),'-module-cache-path',str(out/'module-cache'),'-O',str(root/'kits/workkit/native/keychain.swift'),'-o',str(out/'workkit-keychain-helper')])
toolchains=[]
for command in commands:
    tool=pathlib.Path(command[0]);pin=sha(tool)
    result=subprocess.run([str(tool),'version' if tool==compiler else '--version'],env=env,capture_output=True,timeout=10,check=True)
    if len(result.stdout)>16384: raise SystemExit('compiler version output bound')
    subprocess.run(command,cwd=root,env=env,check=True)
    if sha(tool)!=pin: raise SystemExit('compiler changed during build')
    toolchains.append({'invocationPath':str(tool),'path':str(tool.resolve()),'sha256':pin,'version':result.stdout.decode().strip()})
if snapshot()!=before: raise SystemExit('source drift during build')
if sdk and sha(pathlib.Path(sdk['path'])/'SDKSettings.json')!=sdk['settingsSha256']: raise SystemExit('SDK metadata changed during build')
manifest={'commands':commands,'toolchains':toolchains,'sdk':sdk,'sources':before,'implementationPin':(root/'kits/workkit/internal/engine/implementation_pin.go').read_text(),'binaries':[{'path':p.name,'sha256':sha(p)} for p in out.iterdir() if p.is_file()],'liveQualification':'pending','nativeCertification':'pending'}
(out/'build.json').write_text(json.dumps(manifest,indent=2)+'\n')
