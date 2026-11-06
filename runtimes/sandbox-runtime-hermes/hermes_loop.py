#!/usr/bin/env python3
"""Minimal Hermes-derived loop: tool rounds, MCP and lazy Skills only."""
import argparse, hashlib, json, os, pathlib, shutil, subprocess, sys, tarfile, time, urllib.request, zipfile

ROOT=pathlib.Path('/app'); WS=ROOT/'workspace'; OUT=ROOT/'output'; LIMIT=64<<10
class E(Exception):
 def __init__(s,c,m):s.c,s.m=c,m
def fail(c,m):raise E(c,m)
def js(v):return json.dumps(v,separators=(',',':'),ensure_ascii=False).encode()
def clip(v,n=LIMIT):
 s=v if isinstance(v,str) else json.dumps(v,ensure_ascii=False)
 return s[:n]
def request(url,data,token='',headers=None,timeout=60):
 h={'Content-Type':'application/json',**(headers or {})}
 if token:h['Authorization']='Bearer '+token
 try:
  with urllib.request.urlopen(urllib.request.Request(url,data=js(data),headers=h,method='POST'),timeout=timeout) as r:b=r.read(LIMIT+1)
 except Exception:fail('agent_execution_failed','upstream request failed')
 if len(b)>LIMIT:fail('agent_execution_failed','upstream response too large')
 try:return json.loads(b)
 except Exception:fail('agent_execution_failed','upstream returned invalid JSON')
def rel(p):
 if not isinstance(p,str) or not p or p.startswith('/') or '\\' in p or any(x in ('','.','..') for x in p.split('/')):fail('invalid_request','unsafe path')
 return p
def file(p):
 p=rel(p); x=WS/p
 cur=WS
 for q in pathlib.PurePosixPath(p).parts:
  cur/=q
  if cur.is_symlink():fail('invalid_request','symlink path rejected')
 return x
class Events:
 def __init__(s,c):s.c,s.n=c,0
 def send(s,t,p):
  s.n+=1
  try:request(s.c['runtime']['base_url'].rstrip('/')+'/events',{'execution_id':s.c['execution_id'],'source_seq':s.n,'type':t,'payload':p},s.c['runtime']['token'],timeout=5)
  except Exception:pass
def fetch(r,d):
 if not isinstance(r.get('download_url'),str) or not r['download_url'].startswith(('http://','https://')):fail('resource_invalid_entry','bad download URL')
 d.parent.mkdir(parents=True,exist_ok=True)
 try:
  with urllib.request.urlopen(r['download_url'],timeout=30) as a,d.open('wb') as b:shutil.copyfileobj(a,b)
 except Exception:fail('resource_download_failed','download failed')
 if d.stat().st_size!=r.get('size_bytes') or hashlib.file_digest(d.open('rb'),'sha256').hexdigest()!=r.get('sha256'):fail('resource_hash_mismatch','resource does not match checksum')
def resources(c):
 WS.mkdir(parents=True,exist_ok=True); m=[]
 for r in c.get('files',[]):
  fetch(r,file(r.get('name')));m.append({'role':'system','content':f"Attached file {r['name']!r}; use agent_read_file on demand."})
 for r in c.get('skills',[]):
  n=r.get('name')
  if not isinstance(n,str) or '/' in n or not n:fail('resource_invalid_entry','unsafe skill name')
  a=WS/'.skill-archives'/(n+'.archive');fetch(r,a);d=WS/'.skills'/n;shutil.rmtree(d,ignore_errors=True);d.mkdir(parents=True)
  try:
   if n.endswith('.md'):shutil.copyfile(a,d/'SKILL.md')
   elif zipfile.is_zipfile(a):
    with zipfile.ZipFile(a) as z:
     for x in z.namelist():rel(x)
     z.extractall(d)
   else:
    with tarfile.open(a) as t:
     for x in t.getmembers():
      rel(x.name)
      if x.issym() or x.islnk():fail('resource_invalid_entry','skill symlink')
     t.extractall(d,filter='data')
  except E:raise
  except Exception:fail('resource_invalid_entry','invalid skill archive')
  if not (d/'SKILL.md').is_file():fail('resource_invalid_entry','missing SKILL.md')
  m.append({'role':'system','content':f"Skill {n!r} is available; use agent_load_skill on demand."})
 return m
def schema(n,d,p,req=[]):return {'type':'function','function':{'name':n,'description':d,'parameters':{'type':'object','properties':p,'required':req}}}
def builtin_defs():return [schema('agent_list_files','List sandbox workspace files.',{'path':{'type':'string'}}),schema('agent_read_file','Read a UTF-8 workspace file.',{'path':{'type':'string'}},['path']),schema('agent_write_file','Write a UTF-8 workspace file.',{'path':{'type':'string'},'content':{'type':'string'}},['path','content']),schema('agent_load_skill','Load an attached Skill.',{'name':{'type':'string'}},['name']),schema('agent_request_input','Ask caller for input.',{'kind':{'type':'string','enum':['question','choice','approval']},'prompt':{'type':'string'},'options':{'type':'array','items':{'type':'string'}}},['kind','prompt'])]
def builtin(n,a):
 if n=='agent_list_files':
  d=file(a['path']) if a.get('path') else WS
  return json.dumps([x.relative_to(WS).as_posix() for x in list(d.rglob('*'))[:500] if x.is_file()])
 if n=='agent_read_file':return file(a['path']).read_text(encoding='utf8')[:16<<10]
 if n=='agent_write_file':
  p=file(a['path']);v=a.get('content');
  if not isinstance(v,str) or len(v.encode())>1<<20:fail('tool_execution_failed','invalid content')
  p.parent.mkdir(parents=True,exist_ok=True);tmp=p.with_name('.'+p.name+'.tmp');tmp.write_text(v);tmp.replace(p);return json.dumps({'written':a['path']})
 if n=='agent_load_skill':
  x=a.get('name');
  if not isinstance(x,str) or '/' in x:fail('tool_execution_failed','invalid skill')
  return (WS/'.skills'/x/'SKILL.md').read_text(encoding='utf8')[:LIMIT]
 fail('tool_execution_failed','unknown builtin tool')
class MCP:
 def __init__(s,x):
  s.x,s.id,s.i,s.sid=x,x['id'],0,'';s.proc=None
  if x.get('transport')=='stdio':s.proc=subprocess.Popen([x['command'],*x.get('args',[])],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,env={'PATH':os.environ.get('PATH',''),**x.get('env',{})})
  s.rpc('initialize',{'protocolVersion':'2025-03-26','capabilities':{},'clientInfo':{'name':'hermes-runtime','version':'2'}});s.notify('notifications/initialized')
  allowed=set(x.get('allowed_operations',[]));offered=s.rpc('tools/list',{}).get('tools',[]);s.tools={t['name']:t for t in offered if isinstance(t,dict) and t.get('name') in allowed}
  if allowed-set(s.tools):fail('mcp_unavailable','allowed MCP operation unavailable')
 def rpc(s,m,p):
  s.i+=1;q={'jsonrpc':'2.0','id':s.i,'method':m}
  if p is not None:q['params']=p
  if s.proc:
   s.proc.stdin.write(json.dumps(q)+'\n');s.proc.stdin.flush()
   try:r=json.loads(s.proc.stdout.readline())
   except Exception:fail('mcp_unavailable','invalid MCP stdio response')
  else:
   h={'Accept':'application/json','MCP-Protocol-Version':'2025-03-26'}
   if s.sid:h['Mcp-Session-Id']=s.sid
   r=request(s.x['url'],q,headers=h)
  if r.get('id')!=s.i or 'error' in r or not isinstance(r.get('result'),dict):fail('mcp_unavailable','MCP request failed')
  return r['result']
 def notify(s,m):
  q={'jsonrpc':'2.0','method':m}
  if s.proc:
   s.proc.stdin.write(json.dumps(q)+'\n');s.proc.stdin.flush();return
  h={'Content-Type':'application/json','MCP-Protocol-Version':'2025-03-26'}
  if s.sid:h['Mcp-Session-Id']=s.sid
  try:
   with urllib.request.urlopen(urllib.request.Request(s.x['url'],data=js(q),headers=h,method='POST'),timeout=30):pass
  except Exception:fail('mcp_unavailable','MCP initialization failed')
 def call(s,n,a):return clip(s.rpc('tools/call',{'name':n,'arguments':a}))
def mcps(c):
 clients=[];names={};defs=[]
 for x in c.get('tools',[]):
  if x.get('type')!='mcp':continue
  z=MCP(x);clients.append(z)
  for n,t in z.tools.items():
   public='mcp__'+z.id.replace('-','_')+'__'+n.replace('-','_')
   if public in names:fail('mcp_unavailable','duplicate MCP name')
   names[public]=(z,n);inp=t.get('inputSchema') or t.get('input_schema') or {'type':'object'};defs.append(schema(public,t.get('description') or 'MCP tool',inp.get('properties',{}),inp.get('required',[])))
 return clients,names,defs
def steering(c,msgs,at):
 e,t=c['runtime']['base_url'].rstrip('/'),c['runtime']['token'];r=request(e+'/steers/pull',{'execution_id':c['execution_id'],'after_seq':at},t,timeout=10);items=r.get('items',[])
 if not items:return at
 for x in items:
  if not isinstance(x.get('message'),str):fail('steering_delivery_failed','invalid steering')
  msgs.append({'role':'user','content':x['message']})
 n=r.get('through_seq');
 if not isinstance(n,int) or n<at:fail('steering_delivery_failed','invalid cursor')
 request(e+'/steers/ack',{'execution_id':c['execution_id'],'batch_id':r.get('batch_id'),'incorporated_through_seq':n},t,timeout=10);return n
def checkpoint(c,msgs,at,r):
 OUT.mkdir(parents=True,exist_ok=True);b=js({'messages':msgs,'through_seq':at});p=OUT/'checkpoint.json';p.write_bytes(b);return {'path':'/app/output/checkpoint.json','format':'hermes-runtime/2','sha256':hashlib.sha256(b).hexdigest(),'size_bytes':len(b)},r
def run(c):
 ev=Events(c);msgs=list(c['messages']);at=c.get('steering',{}).get('after_seq',0)
 if c.get('resume'):
  try:q=json.loads(pathlib.Path(c['resume']['checkpoint_path']).read_text());msgs,at=q['messages'],q['through_seq']
  except Exception:fail('resume_failed','invalid checkpoint')
  msgs.append({'role':'user','content':'Input response: '+clip(c['resume'].get('answer'))})
 else:msgs+=resources(c)
 cs,remote,defs=mcps(c);defs=builtin_defs()+defs
 try:
  for _ in range(40):
   at=steering(c,msgs,at);ev.send('runtime.progress',{'phase':'model','value':25});url=c['model']['base_url'].rstrip('/');url=url if url.endswith('/chat/completions') else url+'/chat/completions';body={'model':c['model']['name'],'messages':msgs,'tools':defs,'tool_choice':'auto'}
   if c['model'].get('reasoning_effort'):body['reasoning_effort']=c['model']['reasoning_effort']
   ans=request(url,body,c['model']['token'],timeout=c['limits']['remaining_execution_seconds']);choices=ans.get('choices',[])
   if not choices or not isinstance(choices[0].get('message'),dict):fail('agent_execution_failed','model returned no message')
   a=choices[0]['message'];msgs.append(a);calls=a.get('tool_calls') or []
   if not calls:
    text=clip(a.get('content') or '');ev.send('agent.token',{'delta':text});ev.send('runtime.progress',{'phase':'complete','value':100});return {'status':'ok','summary':text,'delivery':{'status':'not_requested'},'steering':{'incorporated_through_seq':at}}
   for call in calls:
    f=call.get('function',{});n=f.get('name');cid=call.get('id');
    try:args=json.loads(f.get('arguments','{}'))
    except Exception:args={}
    ev.send('agent.tool_call',{'tool_call_id':cid,'tool_name':n,'arguments':args,'status':'started'})
    if n=='agent_request_input':
     cp,r=checkpoint(c,msgs,at,{'kind':args.get('kind','question'),'prompt':args.get('prompt',''),'options':args.get('options',[])});return {'status':'awaiting_input','request':r,'checkpoint':cp,'steering':{'incorporated_through_seq':at}}
    started=time.monotonic()
    try:res=builtin(n,args) if n.startswith('agent_') else remote[n][0].call(remote[n][1],args)
    except Exception as x:res=json.dumps({'error':getattr(x,'m','tool execution failed')})
    msgs.append({'role':'tool','tool_call_id':cid,'content':clip(res)});ev.send('agent.tool_result',{'tool_call_id':cid,'tool_name':n,'result':clip(res),'status':'completed','duration_ms':int((time.monotonic()-started)*1000)})
  fail('max_iterations_exceeded','tool-call limit reached')
 finally:
  for x in cs:
   if x.proc:x.proc.terminate()
def main():
 p=argparse.ArgumentParser();p.add_argument('--config',required=True);c=json.loads(pathlib.Path(p.parse_args().config).read_text())
 try:
  for k in ('execution_id','messages','model','runtime','limits'):
   if k not in c:fail('runtime_protocol_invalid','invalid run.json')
  r=run(c)
 except E as x:r={'status':'error','error_code':x.c,'steering':{'incorporated_through_seq':0}}
 except Exception:r={'status':'error','error_code':'agent_execution_failed','steering':{'incorporated_through_seq':0}}
 OUT.mkdir(parents=True,exist_ok=True);t=OUT/'.result.tmp';t.write_bytes(js(r));t.replace(OUT/'result.json');return 0 if r['status']!='error' else 1
if __name__=='__main__':sys.exit(main())
