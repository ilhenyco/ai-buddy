from pathlib import Path
from reportlab.pdfgen import canvas
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
import pypdfium2 as pdfium
from xml.sax.saxutils import escape
R=Path(__file__).resolve().parents[1];D=R/'docs';T=R/'tmp/team-spec'
pdfmetrics.registerFont(TTFont('Arial','C:/Windows/Fonts/arial.ttf'))
W,H=1100,680
boxes=[(30,220,230,150,['React / TypeScript','Реализован FRONT','Локальный макет → API']),
 (350,180,290,210,['Go API · проект','Сессия, ACL, сценарии','Сообщения, вопросы, tools','Публикация / проверки']),
 (770,40,290,130,['PostgreSQL · проект','Сценарии, history, jobs','ACL, версии, outbox']),
 (770,220,290,120,['RAG worker · проект','Разрешённые chunks / index','Ответ + версии цитат']),
 (770,390,290,110,['Qwen / DeepSeek · выбор','Серверный адаптер','Отдельный режим demo']),
 (350,480,290,125,['Scheduler / outbox · проект','Календарь без LLM','Независимые доставки']),
 (30,480,230,125,['HTTP-мок доступа','Условный сценарий OQ08','MCP — вариант адаптера'])]
edges=[((260,295),(350,295),'HTTPS / JSON'),((640,220),(770,105),'SQL / транзакции'),((640,285),(770,280),'AnswerJob'),((915,340),(915,390),'Контекст'),((495,390),(495,480),'События'),((350,350),(260,510),'Подтверждённое действие'),((640,540),(770,170),'Jobs / outbox')]
pdf=T/'architecture.pdf';c=canvas.Canvas(str(pdf),pagesize=(W,H));svg=[f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}"><rect width="100%" height="100%" fill="white"/>']
for x,y,w,h,lines in boxes:
    c.setFillColorRGB(.94,.96,.98);c.setStrokeColorRGB(.18,.23,.3);c.roundRect(x,H-y-h,w,h,8,fill=1)
    svg.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="8" fill="#f0f5fa" stroke="#303b4c"/>')
    for i,line in enumerate(lines):
        yy=y+31+i*27;c.setFillColorRGB(.1,.13,.18);c.setFont('Arial',16);c.drawCentredString(x+w/2,H-yy,line)
        svg.append(f'<text x="{x+w/2}" y="{yy}" text-anchor="middle" font-family="Arial" font-size="16">{escape(line)}</text>')
for a,b,label in edges:
    c.setStrokeColorRGB(.22,.29,.38);c.setLineWidth(1.5);p=c.beginPath();p.moveTo(a[0],H-a[1]);p.lineTo(b[0],H-b[1]);c.drawPath(p)
    import math
    ang=math.atan2(b[1]-a[1],b[0]-a[0]);p=c.beginPath();p.moveTo(b[0],H-b[1]);p.lineTo(b[0]-9*math.cos(ang-.4),H-b[1]+9*math.sin(ang-.4));p.lineTo(b[0]-9*math.cos(ang+.4),H-b[1]+9*math.sin(ang+.4));p.close();c.setFillColorRGB(.22,.29,.38);c.drawPath(p,fill=1)
    mx=(a[0]+b[0])/2;my=(a[1]+b[1])/2;c.setFont('Arial',11);c.drawCentredString(mx,H-my+5,label)
    svg.append(f'<path d="M {a[0]} {a[1]} L {b[0]} {b[1]}" stroke="#38495f" fill="none"/><text x="{mx}" y="{my-5}" font-family="Arial" text-anchor="middle" font-size="11">{escape(label)}</text>')
c.setFont('Arial',12);c.drawString(30,H-30,'Хомыч · схема контейнеров и модулей · проект серверной реализации · 12.11.2026');c.save();svg.append('</svg>');(D/'architecture/architecture-current.svg').write_text(''.join(svg),'utf-8')
doc=pdfium.PdfDocument(str(pdf));doc[0].render(scale=1.7).to_pil().save(D/'architecture/architecture-current.png');print('Architecture SVG/PNG written')
