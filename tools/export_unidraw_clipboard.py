к"""Build a local copy/paste helper using the clipboard format observed in Unidraw UI."""
from pathlib import Path
import json, base64, time, uuid, html

ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/'output'/'editable-diagrams'
data=json.loads((OUT/'diagram-data.json').read_text(encoding='utf-8'))
stamp=int(time.time()*1000)
elements=[]
def base(id,type,x,y,w,h):
    return dict(id=id,type=type,schema=2,position=dict(x=x,y=y),size=dict(width=w,height=h),version=1,versionNonce=stamp%2147483647,createdAt=stamp,updatedAt=stamp,alpha=100,zIndex=1000+len(elements),rotation=0,groupIds=[],isLocked=False,isDeleted=False,isDTO=True)
def text(s): return '<p>'+html.escape(s).replace('\n','</p><p>')+'</p>'
positions=[(0,0),(0,1100),(2100,1100),(0,2150),(2100,2150)]
for d,(ox,oy) in zip(data,positions):
    ids={i['id']:uuid.uuid4().hex for i in d['items']+d['edges']}
    lookup={i['id']:i for i in d['items']}
    diagram_elements=[]
    for i in d['items']:
        if i['id'].endswith('_caption') and lookup.get(i['id'][:-8],{}).get('kind')=='participant': continue
        k=i['kind']; x=i['x']+ox; y=i['y']+oy; w=i['width']; h=i['height']; font=i.get('font',13)
        if i.get('rotation'):
            x+=5; y+=5; w=85; h=25; font=12
        typ='text' if k=='text' else 'shape'
        e=base(ids[i['id']],typ,x,y,w,h)
        e['text']=text(i['text'])
        e['style']=dict(tc='#23344D',tff='Arial, Segoe UI Emoji',tfs=font,ta='s' if i.get('align')=='left' else 'm',tva='m')
        if typ=='shape':
            e.update(shape='5' if k in ('startEvent','endEvent') else '3' if k=='exclusiveGateway' else '1',seed=1,roughness=0)
            e['style'].update(fc=i['fill'] if i['fill']!='none' else 'transparent',fs='s',sc='#334867',sw=3 if k=='endEvent' else 1.5,ss='s',sr=[8,8,8,8] if k in ('task','userTask','serviceTask') else [0,0,0,0])
        elements.append(e);diagram_elements.append(e)
    for edge in d['edges']:
        pts=[dict(x=x+ox,y=y+oy) for x,y in edge['points']]
        x=min(p['x'] for p in pts); y=min(p['y'] for p in pts)
        e=base(ids[edge['id']],'line',x,y,max(p['x'] for p in pts)-x,max(p['y'] for p in pts)-y)
        e['points']=pts[1:-1]
        e['style']=dict(ss='s',sc='#334867',sw=1.5,fc='transparent',fs='s',lst=0,let=2,lt='c',lsc=1)
        e['tipPoints']={}
        for end,p,key in [('start',pts[0],edge['source']),('end',pts[-1],edge['target'])]:
            b=lookup[key]
            e['tipPoints'][end]=dict(boundToId=ids[key],snappedToEdge=False,position=dict(x=(p['x']-ox-b['x'])/b['width'],y=(p['y']-oy-b['y'])/b['height']),absolutePosition=p)
        elements.append(e);diagram_elements.append(e)
    # Group one scenario at a time; the user can enter or ungroup it normally.
    group=uuid.uuid4().hex
    for e in diagram_elements: e['groupIds']=[group]
payload=dict(type='unidraw/clipboard',boardId='571cb1809647a1bcb787',elements=elements)
raw=json.dumps(payload,ensure_ascii=False,separators=(',',':')).encode('utf-8')
encoded=base64.b64encode(bytes((b+59)%256 for b in raw)).decode()
clip='<span data-unidraw="&lt;--(unidraw-data-v1)'+encoded+'(/unidraw-data-v1)--&gt;"></span>'
(OUT/'unidraw-clipboard.json').write_text(json.dumps(payload,ensure_ascii=False,indent=2),encoding='utf-8')
doc='''<!doctype html><meta charset="utf-8"><title>AI-buddy — копирование в Unidraw</title>
<style>body{font:18px Arial;max-width:760px;margin:50px auto;padding:20px;color:#23344d}button{background:#3558df;color:white;border:0;padding:18px 24px;border-radius:12px;font:inherit;cursor:pointer}a{color:#3558df}</style>
<h1>Пять схем AI-buddy</h1><p>Нажмите кнопку, затем откройте свободное место на доске Unidraw и нажмите Ctrl+V.</p>
<p>Весь путь стажировки, первый вход, команда, доступы и первая задача. Каждая схема сгруппирована отдельно. Блоки, подписи и стрелки редактируются.</p>
<button id="copy">Скопировать схемы для Unidraw</button><p id="status"></p>
<p><a href="README.md">Инструкция для Miro и исходные файлы</a></p>
<script>
const content=CLIP;
document.getElementById('copy').onclick=async()=>{try{await navigator.clipboard.write([new ClipboardItem({'text/html':new Blob([content],{type:'text/html'})})]);document.getElementById('status').textContent='Скопировано. Перейдите в Unidraw и нажмите Ctrl+V.'}catch(e){document.getElementById('status').textContent='Браузер не разрешил копирование: '+e.message}};
</script>'''.replace('CLIP',json.dumps(clip))
(OUT/'copy-to-unidraw.html').write_text(doc,encoding='utf-8')
print(f'{len(elements)} editable elements; 5 scenario groups')
