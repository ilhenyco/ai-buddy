"""Build non-executable BPMN with editable diagram interchange geometry."""
from collections import Counter, defaultdict
from heapq import heappop, heappush
from pathlib import Path
import csv
import json
import re
import sys
from lxml import etree as ET

from processes import PROCESSES, ROLES, TRACE, OPEN

BPMN = "http://www.omg.org/spec/BPMN/20100524/MODEL"
BD = "http://www.omg.org/spec/BPMN/20100524/DI"
DC = "http://www.omg.org/spec/DD/20100524/DC"
DI = "http://www.omg.org/spec/DD/20100524/DI"
XSI = "http://www.w3.org/2001/XMLSchema-instance"
COLOR = "http://bpmn.io/schema/bpmn/biocolor/1.0"
NS = dict(bpmn=BPMN, bpmndi=BD, dc=DC, di=DI, xsi=XSI, bioc=COLOR)
TAG = dict(start="startEvent", end="endEvent", manual="manualTask", user="userTask",
           service="serviceTask", xor="exclusiveGateway", sub="subProcess", parallel="parallelGateway",
           timer="intermediateCatchEvent", timerStart="startEvent")
SIZE = {"start": (36, 36), "timerStart": (36,36), "timer": (36,36),
        "end": (36, 36), "xor": (48, 48), "parallel": (48, 48)}
FILL = {"H": "#FFF3DC", "L": "#F0E8FF", "B": "#E7F5F0", "I": "#E7F1FF", "A": "#EEF2F8",
        "M": "#EDE8FF", "R": "#EEF2F8", "K": "#FFF3DC"}
STEP = 6


def element(parent, ns, tag, **attrs):
    return ET.SubElement(parent, f"{{{ns}}}{tag}", **{k: str(v) for k, v in attrs.items()})


def text_element(parent, ns, tag, text):
    element(parent, ns, tag).text = text


def shape(plane, id, rect, **attrs):
    obj = element(plane, BD, "BPMNShape", id=id+"_di", bpmnElement=id, **attrs)
    bounds(obj, rect)
    return obj


def bounds(obj, rect):
    x, y, w, h = rect
    element(obj, DC, "Bounds", x=x, y=y, width=w, height=h)


def label(parent, rect, style):
    obj = element(parent, BD, "BPMNLabel", labelStyle=style)
    bounds(obj, rect)


def parse_process(spec):
    nodes = {}
    for line in spec["nodes"].strip().splitlines():
        key, role, kind, column, name, *source = line.split("|")
        assert key not in nodes, (spec["id"], key)
        nodes[key] = dict(role=role, kind=kind, col=int(column), name=name,
                          rows=[int(r) for r in source[0].split(",")] if source else [])
    edges = []
    tokens = re.findall(r"\w+>\w+(?:\|.*?)?(?=\s+\w+>\w+|$)",spec["edges"])
    for i, token in enumerate(tokens, 1):
        link, *attrs = token.split("|")
        src, dst = link.split(">")
        name = attrs[0].replace("_", " ") if attrs else ""
        condition = attrs[1] if len(attrs) > 1 else ""
        edges.append(dict(src=src, dst=dst, name=name, condition=condition, key=f"F{i:02}"))
    return nodes, edges


def verify_graph(pid, nodes, edges):
    outgoing, incoming = defaultdict(list), defaultdict(list)
    for e in edges:
        assert e["src"] in nodes and e["dst"] in nodes
        outgoing[e["src"]].append(e)
        incoming[e["dst"]].append(e)
    starts, ends = [], []
    for k, n in nodes.items():
        i, o = incoming[k], outgoing[k]
        if n["kind"] in ("start", "timerStart"):
            starts.append(k)
            assert not i and len(o) == 1, (pid, k)
        elif n["kind"] == "end":
            ends.append(k)
            assert i and not o, (pid, k)
        elif n["kind"] == "xor":
            assert i and len(o) >= 2 and all(e["condition"] and e["name"] for e in o), (pid, k)
            assert sum(e["condition"] == "default" for e in o) == 1, (pid, k)
            assert len({e["condition"] for e in o}) == len(o), (pid, k)
        elif n["kind"] == "parallel":
            assert (len(i) == 1 and len(o) > 1) or (len(i) > 1 and len(o) == 1), (pid, k)
        else:
            assert i and len(o) == 1, (pid, k)
    def traverse(seed, adjacency, key):
        seen = set(seed)
        queue = list(seed)
        while queue:
            for e in adjacency[queue.pop()]:
                if e[key] not in seen:
                    seen.add(e[key])
                    queue.append(e[key])
        return seen
    assert len(starts) == 1 and ends
    assert traverse(starts, outgoing, "dst") == set(nodes), pid
    assert traverse(ends, incoming, "src") == set(nodes), pid
    return incoming, outgoing


def grid_area(rect, pad=0):
    x, y, w, h = rect
    return {(i, j) for i in range((x-pad)//STEP, (x+w+pad)//STEP+1)
            for j in range((y-pad)//STEP, (y+h+pad)//STEP+1)}


def positions(nodes, pool_name=''):
    groups = defaultdict(list)
    for k, n in nodes.items():
        groups[n["role"], n["col"]].append(k)
    roles = [r for r in ROLES if any(n["role"] == r for n in nodes.values())]
    xy, labels, lanes = {}, {}, {}
    width = 204 + max(n["col"] for n in nodes.values()) * 174 + 96
    top = 36
    for role in roles:
        max_slots = max(len(v) for (r,_),v in groups.items() if r == role)
        minimum = max(156 + (max_slots-1)*120, len(ROLES[role])*7.3+20,
                      len(pool_name)*7.3+20 if len(roles)==1 else 0)
        lane_h = int((minimum+5)//6)*6
        lanes[role] = (60, top, width-24, lane_h)
        for (r, col), keys in groups.items():
            if r != role:
                continue
            for slot, k in enumerate(keys):
                node = nodes[k]
                w, h = SIZE.get(node["kind"], (138, 96))
                cx = 168 + col*174
                cy = top + 66 + (max_slots-len(keys))*60 + slot*120
                xy[k] = (cx-w//2, cy-h//2, w, h)
                if node["kind"] in SIZE:
                    label_w = 132 if node["kind"] in ("start","end","timerStart","timer") else 156
                    labels[k] = (cx-label_w//2, cy+h//2+6, label_w, 48)
        top += lane_h
    return xy, labels, lanes, width, top


def ports(rect, with_label, direction):
    x,y,w,h = rect
    cx,cy = x+w//2,y+h//2
    definitions = {
        "R": ((x+w,cy),(x+w+12,cy)), "L": ((x,cy),(x-12,cy)),
        "T": ((cx,y),(cx,y-12)), "B": ((cx,y+h),(cx,y+h+12))
    }
    sides = ("R","T","B","L") if direction == "out" else ("L","T","B","R")
    return [definitions[k] for k in sides if k!="B" or not with_label]


def shorten(points):
    result = []
    for p in points:
        if result and result[-1] == p:
            continue
        if len(result)>1 and any(result[-2][i]==result[-1][i]==p[i] for i in (0,1)):
            result[-1] = p
        else:
            result.append(p)
    return result


def route(src, dst, xy, labels, blocked, used, width, bottom, hints=None):
    # Search on a 6 px grid. Bend cost discourages zigzags and occupied points
    # are penalized so unrelated lines do not run on top of each other.
    def point(p):
        return tuple(v//STEP for v in p)
    def world(p):
        return tuple(v*STEP for v in p)
    sp = ports(xy[src],src in labels,"out")
    dp = ports(xy[dst],dst in labels,"in")
    starts = [(a,point(b),i) for i,(a,b) in enumerate(sp) if point(b) not in blocked]
    ends = {point(b):(a,i) for i,(a,b) in enumerate(dp) if point(b) not in blocked}
    def heur(x,y):
        return min(abs(x-a)+abs(y-b) for a,b in ends)
    assert starts and ends, (src,dst)
    heap, cost, prev, origin = [], {}, {}, {}
    for a,(x,y),i in starts:
        key=(x,y,-1)
        cost[key]=i*4
        origin[key]=a
        heappush(heap,(cost[key]+heur(x,y),cost[key],key))
    best = None
    while heap:
        estimate,g,key=heappop(heap)
        if cost.get(key)!=g:
            continue
        if best and estimate >= best[0]:
            break
        x,y,old=key
        if (x,y) in ends:
            cand = g+4*ends[x,y][1]
            if best is None or cand<best[0]:
                best=(cand,key,ends[x,y][0])
        for d,(dx,dy) in enumerate(((1,0),(0,1),(-1,0),(0,-1))):
            nx,ny=x+dx,y+dy
            if not (14<=nx<=width//STEP+3 and 7<=ny<=(bottom-6)//STEP) or (nx,ny) in blocked:
                continue
            ng=g+1+(6 if old>=0 and d!=old else 0)+min(used[nx,ny],3)*4
            nk=(nx,ny,d)
            if ng<cost.get(nk,float("inf")):
                cost[nk]=ng
                prev[nk]=key
                origin[nk]=origin[key]
                heappush(heap,(ng+heur(nx,ny),ng,nk))
    assert best, (src,dst)
    _,k,target=best
    source=origin[k]
    path=[]
    while True:
        path.append((k[0],k[1]))
        if k not in prev:
            break
        k=prev[k]
    used.update(path)
    return shorten([source,*[world(p) for p in reversed(path)],target])


def edge_caption(name, path, occupied, drawn, width, bottom):
    w = max(30, min(174, len(name)*7 + 12))
    h = 18
    candidates = []
    for (x1,y1),(x2,y2) in zip(path,path[1:]):
        if y1==y2:
            lo,hi=sorted((x1,x2))
            for cx in [(lo+hi)//2,lo+w//2,hi-w//2,(lo+hi)//2-18,(lo+hi)//2+18]:
                for y in [y1-h-7,y1+7,y1-h-16,y1+16]:
                    candidates.append((0 if hi-lo>=w+8 else 70,(cx-w//2,y,w,h)))
        else:
            lo,hi=sorted((y1,y2))
            for at in (.25,.5,.75):
                y=int(lo+(hi-lo)*at)-h//2
                short=0 if hi-lo>=h+16 else 70
                candidates.extend([(10+short,(x1+8,y,w,h)),(12+short,(x1-w-8,y,w,h)),
                                   (16+short,(x1+18,y,w,h)),(18+short,(x1-w-18,y,w,h))])
    def score(item):
        penalty,rect=item
        x,y,w,h=rect
        outside=10000 if x<84 or x+w>width+30 or y<37 or y+h>bottom else 0
        area=grid_area(rect,1)
        return outside+len(area&occupied)*100+len(area&drawn)*45+penalty
    _,best=min(candidates,key=score)
    return best


def create_model(spec, out, checklist):
    pid=spec["id"]
    nodes,edges=parse_process(spec)
    incoming,outgoing=verify_graph(pid,nodes,edges)
    xy,labels,lanes,w,bottom=positions(nodes,spec['title'])
    root=ET.Element(f"{{{BPMN}}}definitions",nsmap=NS,id=f"Defs_{pid}",
                    targetNamespace="urn:origination:ai-buddy:bpmn:v3",
                    exporter="Origination AI-buddy")
    collaboration=element(root,BPMN,"collaboration",id=f"{pid}_collab")
    proc_id=f"{pid}_process"
    pool_id=f"{pid}_pool"
    element(collaboration,BPMN,"participant",id=pool_id,name=spec["title"],processRef=proc_id)
    external_id=f"{pid}_accessService"
    if "external" in spec:
        element(collaboration,BPMN,"participant",id=external_id,name=spec["external"])
        task,req,resp=spec["exchange"]
        element(collaboration,BPMN,"messageFlow",id=f"{pid}_request",name=req,
                sourceRef=f"{pid}_{task}",targetRef=external_id)
        element(collaboration,BPMN,"messageFlow",id=f"{pid}_response",name=resp,
                sourceRef=external_id,targetRef=f"{pid}_{task}")
    p=element(root,BPMN,"process",id=proc_id,name=spec["title"],isExecutable="false")
    text_element(p,BPMN,"documentation","\n".join([
        f"Область: {spec['scope']}",f"Старт: {spec['trigger']}",f"Результат: {spec['result']}",
        f"Приёмка: {spec['acceptance']}",
        'Функциональные требования 0.4: '+', '.join(spec['requirements']),
        *spec["notes"]]))
    ls=element(p,BPMN,"laneSet",id=f"{pid}_lanes")
    for r in lanes:
        l=element(ls,BPMN,"lane",id=f"{pid}_lane_{r}",name=ROLES[r])
        for k,n in nodes.items():
            if n["role"]==r:
                text_element(l,BPMN,"flowNodeRef",f"{pid}_{k}")
    for k,n in nodes.items():
        extra={}
        if n["kind"]=="xor":
            extra["default"]=f"{pid}_{next(e['key'] for e in outgoing[k] if e['condition']=='default')}"
            extra["gatewayDirection"]="Diverging" if len(incoming[k])==1 else "Mixed"
        obj=element(p,BPMN,TAG[n["kind"]],id=f"{pid}_{k}",name=n["name"],**extra)
        requirement_ids=[rid for rid,refs in TRACE.items() if f'{pid}_{k}' in refs]
        if requirement_ids:
            text_element(obj,BPMN,'documentation','Текущие требования: '+', '.join(requirement_ids))
        if k in spec.get('node_notes',{}):
            text_element(obj,BPMN,'documentation',spec['node_notes'][k])
        for r in n["rows"]:
            row=checklist[r-1]
            text_element(obj,BPMN,"documentation",
                         f"Исторический источник: лист 3, строка {r}. {row[0]}. Роль источника: {row[3]}.\n"
                         'После уточнения концепции поведение и роль AI определяются текущими требованиями и этой моделью; старые формулировки источника не являются новым условием выполнения.')
        if n["kind"]=="sub":
            text_element(obj,BPMN,"documentation","Обзорная свёртка. Детали находятся в моделях с указанными идентификаторами.")
        for e in incoming[k]:
            text_element(obj,BPMN,"incoming",f"{pid}_{e['key']}")
        for e in outgoing[k]:
            text_element(obj,BPMN,"outgoing",f"{pid}_{e['key']}")
        if n['kind'] in ('timer','timerStart'):
            td=element(obj,BPMN,'timerEventDefinition',id=f'{pid}_{k}_timer')
            element(td,BPMN,'timeDate',**{f'{{{XSI}}}type':'bpmn:tFormalExpression'}).text='${'+spec['timers'][k]+'}'
    for e in edges:
        attrs={"name":e["name"]} if e["name"] else {}
        obj=element(p,BPMN,"sequenceFlow",id=f"{pid}_{e['key']}",
                    sourceRef=f"{pid}_{e['src']}",targetRef=f"{pid}_{e['dst']}",**attrs)
        if e["condition"] not in ("","default"):
            element(obj,BPMN,"conditionExpression",**{f"{{{XSI}}}type":"bpmn:tFormalExpression"}).text=e["condition"]
    d=element(root,BD,"BPMNDiagram",id=f"{pid}_diagram",name=spec["title"])
    plane=element(d,BD,"BPMNPlane",id=f"{pid}_plane",bpmnElement=f"{pid}_collab")
    shape(plane,pool_id,(30,36,w+6,bottom-36),isHorizontal="true")
    for r,rect in lanes.items():
        shape(plane,f"{pid}_lane_{r}",rect,isHorizontal="true")
    if "external" in spec:
        shape(plane,external_id,(30,bottom+84,w+6,60),isHorizontal="true")
    for k,rect in xy.items():
        n=nodes[k]
        fill=FILL[n["role"]]
        if n["kind"] in ("start","timerStart"): fill="#E1F5E6"
        if n["kind"]=="end": fill="#D6E8ED"
        if n["kind"]=="xor": fill="#FFF1D0"
        attrs={f"{{{COLOR}}}fill":fill,f"{{{COLOR}}}stroke":"#243448"}
        if n["kind"]=="sub":
            attrs["isExpanded"]="false"
        obj=shape(plane,f"{pid}_{k}",rect,**attrs)
        if k in labels:
            label(obj,labels[k],f"{pid}_font")
    blocked=set().union(*(grid_area(r,3) for r in xy.values()),*(grid_area(r,1) for r in labels.values()))
    used=Counter()
    occupied=blocked.copy()
    lane_lines=set().union(*(grid_area((84,y,w-45,0),2) for _,y,_,_ in lanes.values()))
    # Allocate every path before placing text labels. Short paths establish the
    # local reading direction, leaving long bypasses and retries outside it.
    order=sorted(edges,key=lambda e:(abs(nodes[e["dst"]]["col"]-nodes[e["src"]]["col"]),e["key"]))
    paths={}
    for e in order:
        paths[e["key"]]=route(e["src"],e["dst"],xy,labels,blocked,used,w,bottom)
    for e in edges:
        path=paths[e["key"]]
        f_id=f"{pid}_{e['key']}"
        obj=element(plane,BD,"BPMNEdge",id=f_id+"_di",bpmnElement=f_id)
        for x,y in path:
            element(obj,DI,"waypoint",x=x,y=y)
        if e["name"]:
            cap=edge_caption(e["name"],path,occupied,set(used)|lane_lines,w,bottom)
            label(obj,cap,f"{pid}_font")
            occupied.update(grid_area(cap,2))
    if "external" in spec:
        k=spec["exchange"][0]
        x,y,ww,hh=xy[k]
        for suffix,xp,reverse,shift in [("request",x+36,False,-36),("response",x+ww-36,True,32)]:
            points=[(xp,y+hh),(xp,bottom+36),(xp+shift,bottom+36),(xp+shift,bottom+84)]
            obj=element(plane,BD,"BPMNEdge",id=f"{pid}_{suffix}_di",bpmnElement=f"{pid}_{suffix}")
            for xx,yy in reversed(points) if reverse else points:
                element(obj,DI,"waypoint",x=xx,y=yy)
            cap_x = xp+shift-135 if not reverse else xp+shift+5
            label(obj,(cap_x,bottom+48,130,20),f"{pid}_font")
    style=element(d,BD,"BPMNLabelStyle",id=f"{pid}_font")
    element(style,DC,"Font",name="Arial",size=13)
    file=out/f"{pid}.bpmn"
    ET.ElementTree(root).write(file,encoding="UTF-8",xml_declaration=True,pretty_print=True)
    return dict(id=pid,title=spec["title"],scope=spec["scope"],trigger=spec["trigger"],result=spec["result"],
                acceptance=spec["acceptance"],notes=spec["notes"],nodes=nodes,edges=edges,
                requirements=spec['requirements'],open_questions=spec['open_questions'])


def main():
    base=Path(__file__).parent
    out=Path(sys.argv[1])
    out.mkdir(parents=True,exist_ok=True)
    schema=ET.XMLSchema(ET.parse(str(Path(sys.argv[2])/"BPMN20.xsd")))
    checklist=json.loads((base.parent/'bpmn-v2'/"checklist-source.json").read_text(encoding="utf-8"))
    requirements_text=(base.parent/'functional-requirements.md').read_text(encoding='utf-8')
    parts=re.split(r'^### (FR-[A-Z]+-\d+) (.+)$',requirements_text,flags=re.M)
    requirements={parts[i]:dict(name=parts[i+1],status=re.search(r'\*\*Статус:\*\* ([^\n]+)',parts[i+2]).group(1)) for i in range(1,len(parts),3)}
    assert set(requirements)==set(TRACE), sorted(set(requirements)^set(TRACE))
    models=[]
    mapping=defaultdict(list)
    for spec in PROCESSES:
        m=create_model(spec,out,checklist)
        schema.assertValid(ET.parse(str(out/f"{m['id']}.bpmn")))
        models.append(m)
        for k,n in m["nodes"].items():
            for r in n["rows"]:
                mapping[r].append(f"{m['id']}_{k}")
        print(m["id"],len(m["nodes"]),"nodes",len(m["edges"]),"flows",flush=True)
    assert set(mapping)==set(range(2,len(checklist)+1)), sorted(set(range(2,len(checklist)+1))-mapping.keys())
    node_ids={m['id']+'_'+k for m in models for k in m['nodes']}
    assert all(ref in node_ids for refs in TRACE.values() for ref in refs)
    with (out/'requirements-traceability.csv').open('w',encoding='utf-8-sig',newline='') as f:
        writer=csv.writer(f,delimiter=';')
        writer.writerow(['ID требования','Название','Статус','Текущие узлы BPMN'])
        for rid,refs in TRACE.items():
            writer.writerow([rid,requirements[rid]['name'],requirements[rid]['status'],', '.join(refs)])
    with (out/"traceability.csv").open("w",encoding="utf-8-sig",newline="") as f:
        writer=csv.writer(f,delimiter=";")
        writer.writerow(["Строка листа",*checklist[0],"ID элементов BPMN"])
        for r,data in enumerate(checklist[1:],2):
            writer.writerow([r,*data,", ".join(mapping[r])])
    (out/"model-index.json").write_text(json.dumps({"version":3.1,"requirements_version":"0.4","models":models,"rows":mapping,
       'requirements':requirements,'requirement_nodes':TRACE,'open_questions':OPEN},ensure_ascii=False,indent=2),
                                       encoding="utf-8")
    print(f"XSD OK for {len(models)} models. Covered {len(mapping)} checklist rows and {len(TRACE)} functional requirements.")


if __name__=="__main__":
    main()
