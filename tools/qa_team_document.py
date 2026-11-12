"""Rasterize real Word-exported PDF; report bounds and contact sheets for review."""
from pathlib import Path
import json,sys
from PIL import Image,ImageDraw,ImageFont
import pypdfium2 as pdfium
R=Path(__file__).resolve().parents[1];T=R/'tmp/team-spec';O=T/'final-render-v3';O.mkdir(exist_ok=True)
p=pdfium.PdfDocument(str(T/'team-specification-final-v3.pdf'));records=[]
font=ImageFont.truetype('C:/Windows/Fonts/arial.ttf',20)
for i,page in enumerate(p):
    img=page.render(scale=1.45).to_pil();path=O/f'page-{i+1:03}.png';img.save(path)
    textpage=page.get_textpage();text=textpage.get_text_range();w,h=page.get_size()
    bad=[]
    for n in range(textpage.count_chars()):
        char=textpage.get_text_range(n,1)
        if not char.strip():continue
        x0,y0,x1,y1=textpage.get_charbox(n)
        if x0 < -1 or y0 < -1 or x1 > w+1 or y1 > h+1:bad.append(n)
    records.append({'page':i+1,'width':w,'height':h,'characters':len(text),'outside_page':len(bad),'starts':text[:150]})
    textpage.close()
for start in range(0,len(records),4):
    sheet=Image.new('RGB',(1700,2460),'#d8dde4');draw=ImageDraw.Draw(sheet)
    for j,record in enumerate(records[start:start+4]):
        im=Image.open(O/f'page-{record["page"]:03}.png');im.thumbnail((820,1170));x=15+(j%2)*850;y=42+(j//2)*1230
        sheet.paste(im,(x,y));draw.text((x,y-28),f'Page {record["page"]}',font=font,fill='black')
    sheet.save(O/f'contact-{start//4+1:02}.jpg',quality=90)
(T/'document-qa.json').write_text(json.dumps({'pages':len(p),'bounds_errors':sum(x['outside_page'] for x in records),'records':records},ensure_ascii=False,indent=2),'utf-8')
print(json.dumps({'pages':len(p),'contacts':(len(p)+3)//4,'bounds_errors':sum(x['outside_page'] for x in records),'short_pages':[(x['page'],x['characters']) for x in records if x['characters']<100]},ensure_ascii=False))
