"""Render standard BPMN DI without browser automation; preserve editable XML.

PDF is an internal vector render, PNG panels are inserted into Word. Large
graphs get an overview plus numbered horizontal panels with overlap.
"""
from pathlib import Path
import json, math
from lxml import etree as E
from reportlab.pdfgen import canvas
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.lib.colors import HexColor
import pypdfium2 as pdfium
R=Path(__file__).resolve().parents[1]
M=R/'output/bpmn-v3/models'
O=R/'docs/processes/rendered/images'
T=R/'tmp/team-spec/bpmn-render'
NS={'b':'http://www.omg.org/spec/BPMN/20100524/MODEL','bd':'http://www.omg.org/spec/BPMN/20100524/DI','dc':'http://www.omg.org/spec/DD/20100524/DC','di':'http://www.omg.org/spec/DD/20100524/DI'}
pdfmetrics.registerFont(TTFont('Arial','C:/Windows/Fonts/arial.ttf'))

def wrap(text,width,size=13):
    lines=[];line=''
    for w in text.split():
        candidate=(line+' '+w).strip()
        if pdfmetrics.stringWidth(candidate,'Arial',size)>width and line:lines.append(line);line=w
        else:line=candidate
    if line:lines.append(line)
    return lines

def render_one(path):
    root=E.parse(str(path)); elements={x.get('id'):x for x in root.xpath('//*[@id]')}
    shapes=root.xpath('//bd:BPMNShape',namespaces=NS);edges=root.xpath('//bd:BPMNEdge',namespaces=NS)
    def rect(s):return tuple(float(s.find('dc:Bounds',NS).get(k)) for k in ('x','y','width','height'))
    W=max(rect(s)[0]+rect(s)[2] for s in shapes)+40;H=max(rect(s)[1]+rect(s)[3] for s in shapes)+40
    pdf=T/(path.stem+'.pdf');c=canvas.Canvas(str(pdf),pagesize=(W,H));c.setTitle(path.stem+' BPMN')
    def line(points,dash=False,arrow=True):
        c.setStrokeColor(HexColor('#243448'));c.setLineWidth(1.5);c.setDash(5,4) if dash else c.setDash()
        p=c.beginPath();p.moveTo(points[0][0],H-points[0][1]);
        for x,y in points[1:]:p.lineTo(x,H-y)
        c.drawPath(p);c.setDash()
        if arrow:
            x,y=points[-1];px,py=points[-2];a=math.atan2(y-py,x-px)
            p=c.beginPath();p.moveTo(x,H-y)
            for da in (-.42,.42):p.lineTo(x-9*math.cos(a+da),H-(y-9*math.sin(a+da)))
            p.close();c.setFillColor(HexColor('#243448'));c.drawPath(p,stroke=1,fill=0 if dash else 1)
    def text(text,x,y,w,h,size=13):
        lines=wrap(text,w-10,size);height=len(lines)*size*1.18
        if height>h+2:raise ValueError(f'Label overflow {path.stem}: {text!r}: {height}>{h}')
        c.setFont('Arial',size);c.setFillColor(HexColor('#172332'))
        top=y+(h-height)/2+size
        for i,s in enumerate(lines):c.drawCentredString(x+w/2,H-(top+i*size*1.18),s)
    # Pools/lanes first, then arrows under nodes.
    for s in shapes:
        el=elements[s.get('bpmnElement')];kind=E.QName(el).localname
        if kind not in ('participant','lane'):continue
        x,y,w,h=rect(s);c.setStrokeColor(HexColor('#9EAABA'));c.setLineWidth(1);c.setFillColor(HexColor('#FFFFFF'));c.rect(x,H-y-h,w,h,fill=1,stroke=1)
        c.line(x+28,H-y,x+28,H-y-h)
        c.saveState();c.translate(x+18,H-y-h/2);c.rotate(90);c.setFont('Arial',12);c.setFillColor(HexColor('#243448'));c.drawCentredString(0,0,el.get('name',''));c.restoreState()
    for ed in edges:
        el=elements[ed.get('bpmnElement')];pts=[(float(p.get('x')),float(p.get('y'))) for p in ed.findall('di:waypoint',NS)]
        line(pts,E.QName(el).localname=='messageFlow')
        b=ed.find('bd:BPMNLabel/dc:Bounds',NS)
        if b is not None:
            x,y,w,h=[float(b.get(k)) for k in ('x','y','width','height')];c.setFillColor(HexColor('#FFFFFF'));c.rect(x,H-y-h,w,h,fill=1,stroke=0);text(el.get('name',''),x,y,w,h,12)
    for s in shapes:
        el=elements[s.get('bpmnElement')];kind=E.QName(el).localname
        if kind in ('participant','lane'):continue
        x,y,w,h=rect(s);cx=x+w/2;cy=H-y-h/2
        c.setFillColor(HexColor(s.get('{http://bpmn.io/schema/bpmn/biocolor/1.0}fill','#FFFFFF')));c.setStrokeColor(HexColor('#243448'));c.setLineWidth(2)
        if kind.endswith('Event'):
            c.setLineWidth(3.5 if kind=='endEvent' else 1.8);c.circle(cx,cy,w/2,fill=1,stroke=1)
            if kind=='intermediateCatchEvent':c.setLineWidth(1);c.circle(cx,cy,w/2-4,fill=0,stroke=1)
            if el.find('b:timerEventDefinition',NS) is not None:
                c.circle(cx,cy,10,fill=0,stroke=1);c.line(cx,cy,cx,cy+7);c.line(cx,cy,cx+5,cy-3)
        elif kind.endswith('Gateway'):
            p=c.beginPath();p.moveTo(cx,cy+h/2);p.lineTo(cx+w/2,cy);p.lineTo(cx,cy-h/2);p.lineTo(cx-w/2,cy);p.close();c.drawPath(p,fill=1,stroke=1)
            if kind=='parallelGateway':c.line(cx-8,cy,cx+8,cy);c.line(cx,cy-8,cx,cy+8)
            else:c.line(cx-7,cy-7,cx+7,cy+7);c.line(cx-7,cy+7,cx+7,cy-7)
        else:
            c.roundRect(x,H-y-h,w,h,9,fill=1,stroke=1)
            # Standard task-type markers, drawn as vector primitives.
            ix=x+15;iy=H-y-14;c.setLineWidth(1)
            if kind=='userTask':c.circle(ix,iy+2,3,fill=0);c.line(ix,iy-1,ix,iy-6);c.line(ix-5,iy-4,ix+5,iy-4)
            elif kind=='serviceTask':
                c.circle(ix,iy,4,fill=0)
                for a in range(0,360,45):
                    a=math.radians(a);c.line(ix+4*math.cos(a),iy+4*math.sin(a),ix+7*math.cos(a),iy+7*math.sin(a))
            elif kind=='manualTask':
                p=c.beginPath();p.moveTo(ix-6,iy-3)
                for xx,yy in [(-5,3),(-2,3),(-2,7),(0,7),(0,3),(4,3),(6,0),(4,-5),(-3,-5)]:p.lineTo(ix+xx,iy+yy)
                p.close();c.drawPath(p,stroke=1,fill=0)
            if kind=='subProcess':c.rect(cx-6,H-y-h+3,12,12,fill=0,stroke=1);c.line(cx-4,H-y-h+9,cx+4,H-y-h+9);c.line(cx,H-y-h+5,cx,H-y-h+13)
            text(el.get('name',''),x+4,y+17,w-8,h-22,13)
        label=s.find('bd:BPMNLabel/dc:Bounds',NS)
        if label is not None:
            lx,ly,lw,lh=[float(label.get(k)) for k in ('x','y','width','height')];text(el.get('name',''),lx,ly,lw,lh,13)
    c.save();doc=pdfium.PdfDocument(str(pdf));page=doc[0]
    page.render(scale=1.5).to_pil().save(O/(path.stem+'.png'))
    panels=[]
    if W>1530:
        # Cut between DI node columns, keep 100px context on each continuation.
        cols=sorted({round(rect(s)[0]+rect(s)[2]/2) for s in shapes if E.QName(elements[s.get('bpmnElement')]).localname not in ('participant','lane')})
        cuts=[0];start=0
        while W-start>1500:
            candidates=[(a+b)/2 for a,b in zip(cols,cols[1:]) if 900<(a+b)/2-start<1420]
            cut=max(candidates) if candidates else start+1300
            cuts.append(cut);start=cut
        cuts.append(W)
        for i,(left,right) in enumerate(zip(cuts,cuts[1:]),1):
            left=max(0,left-60);right=min(W,right+60)
            img=page.render(scale=1.8,crop=(left,0,W-right,0)).to_pil()
            name=f'{path.stem}-detail-{i:02}.png';img.save(O/name);panels.append(name)
    return {'id':path.stem,'image':path.stem+'.png','width':W,'height':H,'panels':panels}

def main():
    O.mkdir(parents=True,exist_ok=True);T.mkdir(parents=True,exist_ok=True)
    records=[render_one(p) for p in sorted(M.glob('P*.bpmn'))]
    (O.parent/'render-index.json').write_text(json.dumps(records,ensure_ascii=False,indent=2),'utf-8');print(f'Rendered {len(records)} BPMN and {sum(len(x["panels"]) for x in records)} detail panels')
if __name__=='__main__':main()
