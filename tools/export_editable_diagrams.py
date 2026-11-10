"""Export existing BPMN DI to editable draw.io and Visio shapes; no process changes."""
from pathlib import Path
import xml.etree.ElementTree as E
import zipfile
import json
import math
import textwrap

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'output' / 'editable-diagrams'
OUT.mkdir(parents=True, exist_ok=True)
NS = dict(b='http://www.omg.org/spec/BPMN/20100524/MODEL', d='http://www.omg.org/spec/BPMN/20100524/DI', dc='http://www.omg.org/spec/DD/20100524/DC', di='http://www.omg.org/spec/DD/20100524/DI')
FILES = [('internship-journey','00 · Весь путь стажировки'),('onboarding-entry','01 · Первый вход'),('onboarding-team','02 · Знакомство с командой'),('onboarding-access','03 · Корпоративные доступы'),('onboarding-first-task','04 · Первая задача')]

def bounds(el):
    return {k:float(el.get(k)) for k in ('x','y','width','height')}

def read(name,title):
    tree=E.parse(ROOT/'docs'/'ai-buddy'/f'{name}.bpmn')
    sem={e.get('id'):e for e in tree.iter() if e.get('id')}
    items=[]; edges=[]
    for di in tree.findall('.//d:BPMNShape',NS):
        id=di.get('bpmnElement'); e=sem[id]; kind=e.tag.split('}')[-1]
        label=e.get('name','') or e.findtext('b:text','',NS)
        b=bounds(di.find('dc:Bounds',NS))
        fill=di.get('{http://bpmn.io/schema/bpmn/biocolor/1.0}fill','#ffffff')
        item=dict(id=id,kind=kind,text=label,fill=fill,**b)
        if kind in ('participant','lane'):
            item['text']=''; item['fill']='#ffffff' if kind=='participant' else '#f5f8fc'
            items.append(item)
            items.append(dict(id=id+'_caption',kind='text',text=label,fill='none',x=b['x'],y=b['y'],width=30,height=b['height'],rotation=270))
        else:
            lb=di.find('d:BPMNLabel/dc:Bounds',NS)
            if lb is not None:
                item['text']='×' if kind=='exclusiveGateway' else ''
                items.append(item)
                items.append(dict(id=id+'_caption',kind='text',text=label,fill='none',**bounds(lb)))
            else: items.append(item)
    for di in tree.findall('.//d:BPMNEdge',NS):
        id=di.get('bpmnElement'); e=sem[id]
        points=[(float(p.get('x')),float(p.get('y'))) for p in di.findall('di:waypoint',NS)]
        edges.append(dict(id=id,source=e.get('sourceRef'),target=e.get('targetRef'),text=e.get('name',''),points=points))
        lb=di.find('d:BPMNLabel/dc:Bounds',NS)
        if lb is not None and e.get('name'):
            items.append(dict(id=id+'_caption',kind='text',text=e.get('name'),fill='#ffffff',**bounds(lb)))
            edges[-1]['text']=''
    w=max(i['x']+i['width'] for i in items)+40
    h=max(i['y']+i['height'] for i in items)+65
    # Title and provenance remain editable objects outside the process pool.
    items.append(dict(id='page_title',kind='text',text=title,fill='none',x=55,y=-55,width=min(w-80,950),height=35,font=24,align='left'))
    items.append(dict(id='page_note',kind='text',text='AI-buddy · Проектная модель на основе чек-листа Авито. Правила прототипа — решения команды.',fill='none',x=55,y=h-35,width=min(w-80,1100),height=28,font=12,align='left'))
    # Add margin above the original coordinates without altering relative geometry.
    for i in items: i['y']+=95
    if name=='onboarding-access':
        label=next(i for i in items if i['id']=='D40_caption')
        label.update(x=1360,y=416,width=155,height=25)
    if name=='onboarding-first-task':
        label=next(i for i in items if i['id']=='D53_caption')
        label.update(x=677,y=166,width=132,height=25)
    for e in edges: e['points']=[(x,y+95) for x,y in e['points']]
    return dict(name=name,title=title,items=items,edges=edges,width=w,height=h+95)

def drawio(data):
    model=E.Element('mxGraphModel',grid='1',gridSize='10',page='0',pageWidth=str(data['width']),pageHeight=str(data['height']),connect='1',arrows='1')
    root=E.SubElement(model,'root'); E.SubElement(root,'mxCell',id='0'); E.SubElement(root,'mxCell',id='1',parent='0')
    lookup={i['id']:i for i in data['items']}
    for i in data['items']:
        kind=i['kind']; style=f"whiteSpace=wrap;html=1;fontFamily=Arial;fontSize={i.get('font',13)};fontColor=#23344D;strokeColor=#334867;fillColor={i['fill']};strokeWidth=1.5;"
        if kind=='text': style+=f"shape=label;strokeColor=none;spacing=0;align={i.get('align','center')};verticalAlign=middle;"
        elif kind in ('startEvent','endEvent'): style+='shape=ellipse;aspect=fixed;'+('strokeWidth=3;' if kind=='endEvent' else '')
        elif kind=='exclusiveGateway': style+='shape=rhombus;fontSize=22;'
        elif kind in ('participant','lane'): style+='rounded=0;'
        elif kind=='textAnnotation': style+='shape=note;fillColor=#FFF8DB;fontSize=12;align=left;spacing=8;'
        else: style+='rounded=1;arcSize=15;spacing=8;'
        if i.get('rotation'): style+='horizontal=0;'
        cell=E.SubElement(root,'mxCell',id=i['id'],value=i['text'],style=style,vertex='1',parent='1',bpmnType=kind)
        E.SubElement(cell,'mxGeometry',**{k:str(i[k]) for k in ('x','y','width','height')},attrib={'as':'geometry'})
    for e in data['edges']:
        st='edgeStyle=none;rounded=0;html=0;endArrow=block;endFill=1;strokeWidth=1.5;strokeColor=#334867;fontFamily=Arial;fontSize=12;labelBackgroundColor=#ffffff;'
        for role,p,key in [('exit',e['points'][0],e['source']),('entry',e['points'][-1],e['target'])]:
            b=lookup[key]; st+=f"{role}X={(p[0]-b['x'])/b['width']};{role}Y={(p[1]-b['y'])/b['height']};{role}Perimeter=0;"
        cell=E.SubElement(root,'mxCell',id=e['id'],value=e['text'],style=st,edge='1',parent='1',source=e['source'],target=e['target'])
        g=E.SubElement(cell,'mxGeometry',relative='1',attrib={'as':'geometry'})
        a=E.SubElement(g,'Array',attrib={'as':'points'})
        for x,y in e['points'][1:-1]: E.SubElement(a,'mxPoint',x=str(x),y=str(y))
    return model

def save_drawio(datas,path):
    doc=E.Element('mxfile',host='app.diagrams.net',agent='AI-buddy BPMN export',version='26.0.0')
    for d in datas:
        E.SubElement(doc,'diagram',id=d['name'],name=d['title']).append(drawio(d))
    E.indent(doc); E.ElementTree(doc).write(path,encoding='utf-8',xml_declaration=True)

V='http://schemas.microsoft.com/office/visio/2012/main'
E.register_namespace('',V)
R='http://schemas.openxmlformats.org/officeDocument/2006/relationships'
E.register_namespace('r',R)
PKG='http://schemas.openxmlformats.org/package/2006/relationships'
CT='http://schemas.openxmlformats.org/package/2006/content-types'
def ve(tag,parent=None,**a):
    return E.Element('{'+V+'}'+tag,a) if parent is None else E.SubElement(parent,'{'+V+'}'+tag,a)
def cell(p,n,v,f=None):
    a=dict(N=n,V=str(v))
    if f is not None: a['F']=f
    return ve('Cell',p,**a)
def rels(rows):
    p=E.Element('Relationships',xmlns=PKG)
    for id,type,target in rows: E.SubElement(p,'Relationship',Id=id,Type='http://schemas.microsoft.com/visio/2010/relationships/'+type,Target=target)
    return p
def xml(el): return E.tostring(el,encoding='utf-8',xml_declaration=True)

def vsdx_page(d):
    p=ve('PageContents'); shapes=ve('Shapes',p); ids={i['id']:str(n+1) for n,i in enumerate(d['items']+d['edges'])}
    px=96; height=d['height']/px
    for i in d['items']:
        k=i['kind']; w=i['width']/px; h=i['height']/px
        s=ve('Shape',shapes,ID=ids[i['id']],NameU=i['id'],Type='Shape',LineStyle='0',FillStyle='0',TextStyle='0')
        values=dict(PinX=(i['x']+i['width']/2)/px,PinY=height-(i['y']+i['height']/2)/px,Width=w,Height=h,LocPinX=w/2,LocPinY=h/2,Angle=0,LineWeight=(3 if k=='endEvent' else 1.5)/px,LineColor='#334867',LinePattern=0 if k=='text' else 1,FillForegnd='#ffffff' if i['fill']=='none' else i['fill'],FillPattern=0 if i['fill']=='none' else 1,VerticalAlign=1,LeftMargin=.04,RightMargin=.04,TopMargin=.02,BottomMargin=.02)
        if k.endswith('Task') or k=='task': values['Rounding']=.1
        for n,v in values.items(): cell(s,n,v)
        if i.get('rotation'):
            for n,v in dict(TxtPinX=w/2,TxtPinY=h/2,TxtWidth=h,TxtHeight=w,TxtLocPinX=h/2,TxtLocPinY=w/2,TxtAngle=math.pi/2).items(): cell(s,n,v)
        sec=ve('Section',s,N='Character'); row=ve('Row',sec,IX='0')
        for n,v in dict(Font=0,Color='#23344D',Size=i.get('font',13)/px,LangID=1049).items(): cell(row,n,v)
        sec=ve('Section',s,N='Paragraph'); row=ve('Row',sec,IX='0'); cell(row,'HorzAlign',0 if i.get('align')=='left' else 1)
        sec=ve('Section',s,N='Geometry',IX='0'); cell(sec,'NoFill',1 if i['fill']=='none' else 0); cell(sec,'NoLine',1 if k=='text' else 0)
        if k in ('startEvent','endEvent'):
            row=ve('Row',sec,T='Ellipse',IX='1')
            for n,v in dict(X=w/2,Y=h/2,A=w,B=h/2,C=w/2,D=h).items(): cell(row,n,v)
        else:
            pts=[(0,0),(w,0),(w,h),(0,h),(0,0)]
            if k=='exclusiveGateway': pts=[(0,h/2),(w/2,h),(w,h/2),(w/2,0),(0,h/2)]
            for j,(x,y) in enumerate(pts):
                row=ve('Row',sec,T='MoveTo' if j==0 else 'LineTo',IX=str(j+1)); cell(row,'X',x); cell(row,'Y',y)
        label=i['text']
        if k in ('task','userTask','serviceTask','textAnnotation') and label:
            label='\n'.join(textwrap.wrap(label,width=max(8,int((i['width']-10)/(i.get('font',13)*.53))),break_long_words=False,break_on_hyphens=False))
        ve('Text',s).text=label
    connects=ve('Connects',p)
    for e in d['edges']:
        pts=[(x/px,height-y/px) for x,y in e['points']]; x0=min(x for x,y in pts); y0=min(y for x,y in pts); w=max(x for x,y in pts)-x0; h=max(y for x,y in pts)-y0
        s=ve('Shape',shapes,ID=ids[e['id']],NameU=e['id'],Type='Shape',LineStyle='0',FillStyle='0',TextStyle='0')
        for n,v in dict(PinX=x0+w/2,PinY=y0+h/2,Width=w,Height=h,LocPinX=w/2,LocPinY=h/2,Angle=0,ObjType=2,BeginX=pts[0][0],BeginY=pts[0][1],EndX=pts[-1][0],EndY=pts[-1][1],LineColor='#334867',LineWeight=1.5/px,LinePattern=1,FillPattern=0,EndArrow=4,EndArrowSize=1).items(): cell(s,n,v)
        sec=ve('Section',s,N='Geometry',IX='0'); cell(sec,'NoFill',1)
        for j,(x,y) in enumerate(pts):
            row=ve('Row',sec,T='MoveTo' if j==0 else 'LineTo',IX=str(j+1)); cell(row,'X',x-x0); cell(row,'Y',y-y0)
        if e['text']: ve('Text',s).text=e['text']
        for end,target,part in [('BeginX',e['source'],'9'),('EndX',e['target'],'12')]:
            ve('Connect',connects,FromSheet=ids[e['id']],FromCell=end,FromPart=part,ToSheet=ids[target],ToCell='PinX',ToPart='3')
    return p

def save_vsdx(datas,path):
    doc=ve('VisioDocument'); faces=ve('FaceNames',doc); ve('FaceName',faces,ID='0',Name='Arial',UnicodeRanges='-1 -1 -1 -1',CharSets='0',Panose='2 11 6 4 2 2 2 2 2 4',Flags='325')
    styles=ve('StyleSheets',doc); style=ve('StyleSheet',styles,ID='0',NameU='No Style',Name='No Style')
    for n,v in dict(LineColor='#334867',LinePattern=1,FillForegnd='#ffffff',FillPattern=1).items(): cell(style,n,v)
    pages=ve('Pages'); page_rels=[]
    types=E.Element('Types',xmlns=CT); E.SubElement(types,'Default',Extension='rels',ContentType='application/vnd.openxmlformats-package.relationships+xml'); E.SubElement(types,'Default',Extension='xml',ContentType='application/xml')
    def ctype(p,t): E.SubElement(types,'Override',PartName='/'+p,ContentType='application/vnd.ms-visio.'+t+'+xml')
    ctype('visio/document.xml','drawing.main'); ctype('visio/pages/pages.xml','pages')
    parts={'visio/document.xml':doc,'_rels/.rels':rels([('rId1','document','visio/document.xml')]),'visio/_rels/document.xml.rels':rels([('rId1','pages','pages/pages.xml')])}
    for n,d in enumerate(datas,1):
        page=ve('Page',pages,ID=str(n-1),NameU=d['name'],Name=d['title'])
        sheet=ve('PageSheet',page)
        for k,v in dict(PageWidth=d['width']/96,PageHeight=d['height']/96,PageScale=1,DrawingScale=1,DrawingSizeType=0).items(): cell(sheet,k,v)
        E.SubElement(page,'{'+V+'}Rel',{'{'+R+'}id':f'rId{n}'})
        page_rels.append((f'rId{n}','page',f'page{n}.xml')); parts[f'visio/pages/page{n}.xml']=vsdx_page(d); ctype(f'visio/pages/page{n}.xml','page')
    parts['visio/pages/pages.xml']=pages; parts['visio/pages/_rels/pages.xml.rels']=rels(page_rels); parts['[Content_Types].xml']=types
    with zipfile.ZipFile(path,'w',zipfile.ZIP_DEFLATED) as z:
        for name,el in parts.items(): z.writestr(name,xml(el))

if __name__=='__main__':
    datas=[read(*f) for f in FILES]
    for d in datas:
        save_drawio([d],OUT/(d['name']+'.drawio')); save_vsdx([d],OUT/(d['name']+'.vsdx'))
    save_drawio(datas,OUT/'ai-buddy-all.drawio'); save_vsdx(datas,OUT/'ai-buddy-all.vsdx')
    (OUT/'diagram-data.json').write_text(json.dumps(datas,ensure_ascii=False,indent=2),encoding='utf-8')
    print(json.dumps([{k:d[k] for k in ('name','width','height')}|{'objects':len(d['items']),'connectors':len(d['edges'])} for d in datas],indent=2))
