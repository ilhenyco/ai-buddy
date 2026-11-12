from pathlib import Path
import json,hashlib,re
R=Path(__file__).resolve().parents[1]
chosen=[R/'.gitignore',R/'README.md',R/'.github/workflows/frontend.yml',R/'output/ai-buddy-team-specification.docx']
for folder in ('frontend','docs/ai-buddy','tools','output/bpmn-v3/models'):
    for p in (R/folder).rglob('*'):
        if not p.is_file():continue
        rel=p.relative_to(R)
        if set(rel.parts)&{'node_modules','dist','__pycache__','.git','tmp'}:continue
        if p.name in ('.npmrc','read_handoff_payload.py') or p.name.startswith('.env'):continue
        if p.suffix.lower() in ('.pyc','.zip','.log','.tmp'):continue
        chosen.append(p)
remote=json.loads((R/'tmp/team-spec/remote-files.json').read_text('utf-8'))
entries=[]
for p in sorted(set(chosen)):
    rel=p.relative_to(R).as_posix();data=p.read_bytes();sha=hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()
    if remote.get(rel)==sha:continue
    if p.suffix.lower() in ('.md','.json','.yaml','.yml','.ts','.tsx','.js','.jsx','.mjs','.cjs','.py','.css','.html','.bpmn','.svg','.csv','.txt'):
        txt=data.decode('utf-8-sig')
        assert not re.search(r'(gh[pousr]_[A-Za-z0-9]{25,}|sk-[A-Za-z0-9]{35,}|_authToken\s*=\s*[^$\s]+|BEGIN (?:RSA |EC )?PRIVATE KEY)',txt),f'Possible secret: {rel}'
    entries.append({'path':rel,'size':len(data),'sha':sha})
(R/'tmp/team-spec/publish-manifest.json').write_text(json.dumps(entries,ensure_ascii=False,indent=2),'utf-8')
print(json.dumps({'changed_files':len(entries),'bytes':sum(x['size'] for x in entries),'largest':sorted(entries,key=lambda x:x['size'],reverse=True)[:4]},ensure_ascii=False))
