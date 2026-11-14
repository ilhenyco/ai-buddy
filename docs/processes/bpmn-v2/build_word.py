"""Assemble the product context and the verified BPMN images into Word."""
from pathlib import Path
import json
import re
import sys
from zipfile import ZIP_DEFLATED, ZipFile

from PIL import Image
from docx import Document
from docx.enum.style import WD_STYLE_TYPE
from docx.enum.section import WD_ORIENT, WD_SECTION
from docx.enum.table import WD_TABLE_ALIGNMENT, WD_CELL_VERTICAL_ALIGNMENT
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.opc.constants import RELATIONSHIP_TYPE as RT
from docx.shared import Cm, Inches, Pt, RGBColor

ROOT = Path(__file__).resolve().parents[3]
PROJECT = ROOT / "docs" / "product"
SHEET = ("https://docs.google.com/spreadsheets/d/"
         "1NWVMqilKgqZi8yAycCZPl8OPpLT64qWxFunl_TIbkYc/edit#gid=1846725631")
OMG = "https://www.omg.org/spec/BPMN/2.0.2/"


def ox(tag, **attrs):
    e = OxmlElement(tag)
    for name, value in attrs.items():
        e.set(qn("w:"+name),str(value))
    return e


def hyperlink(p, label, url):
    link = ox("w:hyperlink")
    link.set(qn("r:id"),p.part.relate_to(url,RT.HYPERLINK,is_external=True))
    run = ox("w:r")
    prop = ox("w:rPr")
    prop.append(ox("w:color",val="255777"))
    prop.append(ox("w:u",val="single"))
    run.append(prop)
    t = ox("w:t")
    t.text = label
    run.append(t)
    link.append(run)
    p._p.append(link)


def inline(p, text):
    for part in re.split(r"(\*\*[^*]+\*\*|\[[^\]]+\]\((?:<[^>]+>|[^)]+)\)|`[^`]+`)", text):
        if not part:
            continue
        if part.startswith("**"):
            p.add_run(part[2:-2]).bold = True
        elif part.startswith("[") and "](" in part:
            title, href = part[1:-1].split("](",1)
            href=href.strip("<>")
            if href.startswith("https://"):
                hyperlink(p,title,href)
            else:
                p.add_run(title)
        elif part.startswith("`"):
            p.add_run(part[1:-1])
        else:
            p.add_run(part)
    return p


def para(doc, text, style=None):
    return inline(doc.add_paragraph(style=style),text)


def field(p, name):
    r=p.add_run()
    r._r.append(ox("w:fldChar",fldCharType="begin"))
    inst=ox("w:instrText")
    inst.text=f" {name} "
    r._r.append(inst)
    r._r.append(ox("w:fldChar",fldCharType="separate"))
    val=ox("w:t")
    val.text="1"
    r._r.append(val)
    r._r.append(ox("w:fldChar",fldCharType="end"))


def setup(doc):
    for n in ["Normal","Title","Subtitle","Heading 1","Heading 2","Header","Footer","List Bullet","List Number"]:
        style=doc.styles[n]
        style.font.name="Arial"
        style.font.color.rgb=RGBColor.from_string("000000")
    normal=doc.styles["Normal"]
    normal.font.size=Pt(10.5)
    normal.paragraph_format.space_after=Pt(7)
    normal.paragraph_format.line_spacing=1.12
    for n,size,before,after in [("Title",30,0,10),("Heading 1",18,15,7),("Heading 2",13,9,5),("Subtitle",12,0,11)]:
        style=doc.styles[n]
        style.font.size=Pt(size)
        style.font.bold=n!="Subtitle"
        style.paragraph_format.space_before=Pt(before)
        style.paragraph_format.space_after=Pt(after)
        style.paragraph_format.keep_with_next=True
        ppr=style._element.get_or_add_pPr()
        for b in ppr.findall(qn("w:pBdr")):
            ppr.remove(b)
    for name, size in [("Table Text",9.5),("Diagram Notes",10.5),("Kicker",9.5)]:
        s=doc.styles.add_style(name,WD_STYLE_TYPE.PARAGRAPH)
        s.base_style=normal
        s.font.name="Arial"
        s.font.size=Pt(size)
        s.paragraph_format.space_after=Pt(3)
        s.paragraph_format.line_spacing=1.07
    doc.styles["Kicker"].font.bold=True
    doc.styles["Kicker"].font.color.rgb=RGBColor.from_string("475569")
    doc.styles["Kicker"].paragraph_format.space_before=Pt(2)
    doc.styles["Kicker"].paragraph_format.space_after=Pt(7)
    s=doc.sections[0]
    s.page_width=Cm(21)
    s.page_height=Cm(29.7)
    s.left_margin=s.right_margin=Cm(2)
    s.top_margin=Cm(1.8)
    s.bottom_margin=Cm(1.7)
    s.header_distance=Cm(.85)
    s.footer_distance=Cm(.8)
    hp=s.header.paragraphs[0]
    hp.text="Origination  /  Hack&Change 2026"
    hp.runs[0].font.size=Pt(8)
    fp=s.footer.paragraphs[0]
    fp.alignment=WD_ALIGN_PARAGRAPH.RIGHT
    fp.add_run("AI-buddy   ·   ").font.size=Pt(8)
    field(fp,"PAGE")


def table(doc, rows, widths):
    tbl=doc.add_table(rows=0,cols=len(widths))
    tbl.alignment=WD_TABLE_ALIGNMENT.CENTER
    tbl.autofit=False
    for col,w in zip(tbl.columns,widths):
        col.width=Inches(w)
    pr=tbl._tbl.tblPr
    borders=ox("w:tblBorders")
    for k in ["top","left","bottom","right","insideH","insideV"]:
        borders.append(ox("w:"+k,val="single",sz=4,color="D9D9D9"))
    pr.append(borders)
    margins=ox("w:tblCellMar")
    for k,v in [("top",75),("bottom",65),("left",105),("right",105)]:
        margins.append(ox("w:"+k,w=v,type="dxa"))
    pr.append(margins)
    for i, values in enumerate(rows):
        row=tbl.add_row()
        trpr=row._tr.get_or_add_trPr()
        trpr.append(ox("w:cantSplit"))
        if i==0:
            trpr.append(ox("w:tblHeader",val="true"))
        for cell,value,width in zip(row.cells,values,widths):
            cell.width=Inches(width)
            cell.vertical_alignment=WD_CELL_VERTICAL_ALIGNMENT.CENTER
            cp=cell.paragraphs[0]
            cp.style="Table Text"
            inline(cp,str(value))
            if i==0:
                cell._tc.get_or_add_tcPr().append(ox("w:shd",fill="E5EDF6"))
                for run in cp.runs:
                    run.bold=True
    gap=doc.add_paragraph()
    gap.paragraph_format.space_after=Pt(3)
    gap.paragraph_format.line_spacing=Pt(1)
    gap.add_run().font.size=Pt(1)
    return tbl


def markdown(doc, body):
    lines=body.splitlines()
    pos=0
    while pos<len(lines):
        line=lines[pos].strip()
        if not line:
            pos+=1
            continue
        if line.startswith("|"):
            rows=[]
            while pos<len(lines) and lines[pos].strip().startswith("|"):
                cells=[c.strip() for c in lines[pos].strip().strip("|").split("|")]
                if not all(re.fullmatch(r"[:\- ]+",c) for c in cells):
                    rows.append(cells)
                pos+=1
            cols=len(rows[0])
            widths=(1.42,5.26) if cols==2 else (1.3,2.4,2.98)
            table(doc,rows,widths)
            continue
        if line.startswith("## "):
            heading=line[3:].replace(". "," ",1)
            doc.add_heading(heading,1)
        elif line.startswith("- "):
            para(doc,line[2:],"List Bullet")
        elif not line.startswith("# "):
            para(doc,line)
        pos+=1


def landscape(doc):
    section=doc.add_section(WD_SECTION.NEW_PAGE)
    section.orientation=WD_ORIENT.LANDSCAPE
    section.page_width=Cm(42)
    section.page_height=Cm(29.7)
    section.left_margin=section.right_margin=Cm(1.6)
    section.top_margin=Cm(1.55)
    section.bottom_margin=Cm(1.55)
    section.header_distance=Cm(.65)
    section.footer_distance=Cm(.65)
    return section


def picture(doc, src, width, height, alt):
    with Image.open(src) as im:
        w,h=im.size
    size=min(width/w,height/h)
    p=doc.add_paragraph()
    p.alignment=WD_ALIGN_PARAGRAPH.CENTER
    p.paragraph_format.space_after=Pt(8)
    inline_shape=p.add_run().add_picture(str(src),width=Inches(w*size))
    inline_shape._inline.docPr.set("descr",alt)


def handoff(doc):
    doc.add_heading("Задания по схемам",1)
    para(doc,"Модели описывают действия и переходы для разработки. Исполнение переходов в прототипе обеспечивает Go API и состояние в PostgreSQL. Ручные действия обозначают передачу ответственности между людьми. Их результат поступает в продукт только из согласованного источника или по явному действию ответственного.")
    table(doc,[
        ["Пакет","Что реализовать и проверить"],
        ["T1 · P02","Тестовая сессия и чтение опубликованного плана. Проверить изоляцию команд, ошибку входа и отсутствие плана."],
        ["T2 · P04–P06","Каталог, черновик, подтверждение, HTTP-имитация и чтение статуса. Проверить дубли, потерю ответа, отказ, повтор после перезагрузки и устаревшую ревизию."],
        ["T3 · P03 и P07","Карточки команды и задачи, прогресс и блокировка. Проверить явные отметки, чужой ресурс и HTTP 409 до одобрения нужного ресурса."],
        ["T4 · P14","Ответы со ссылками на разрешённые источники, предсказуемый режим демо и проверка Ollama. Проверить отсутствие источника, тайм-аут и неверный ID."],
        ["T5 · P02–P07 и P14","Повторить весь маршрут с чистого запуска. Собрать отчёт покрытия и приложить инструкции, демонстрацию и презентацию."],
    ],[1.55,5.13])
    doc.add_heading("Границы работ полной версии",2)
    table(doc,[
        ["Схемы","Задание и условия начала"],
        ["P01","Спроектировать редактор плана и права руководителя. Входы — стажёр, команда, дата, бадди и длительность. Выход — опубликованная версия для стажёра. Приёмка — черновик скрыт, публикация разрешена только ответственному, HR может передать ссылку."],
        ["P08 и P10","Определить источник курсов, целей и прогресса. Назначения и итоги встреч подтверждают люди. Продукт показывает данные, а модель по запросу готовит сводку из разрешённых материалов."],
        ["P11 и P13","Согласовать хранение даты, критериев и статуса решения. Продукт отображает зафиксированные значения. Отложенное решение видно вместе со сроком и контактом. Переход статуса проверяется на повторном входе."],
        ["P09 и P12","Шаги учебной практики, итоговой встречи и передачи задач выполняют сотрудники. Они служат условиями и выходами бизнес-процесса. Интерфейсы и интеграции для оформления требуют согласования с HR и владельцами сервисов."],
    ],[1.55,5.13])
    para(doc,"Идентификаторы вида P05_call постоянны внутри поставки. По ним можно найти элемент в исходном BPMN и в таблице соответствия. Номера T1–T5 относятся к плану работ из handoff.md.")


def main():
    models_dir, image_dir, output = (Path(p) for p in sys.argv[1:4])
    index=json.loads((models_dir/"model-index.json").read_text(encoding="utf-8"))
    checklist=json.loads((Path(__file__).parent/"checklist-source.json").read_text(encoding="utf-8"))
    reports=json.loads((image_dir/"render-report.json").read_text(encoding="utf-8"))
    doc=Document()
    setup(doc)
    doc.core_properties.title="AI buddy Контекст продукта и процессы для разработки"
    doc.core_properties.subject="Origination · Hack&Change 2026"
    doc.core_properties.author="Origination"
    para(doc,"AI buddy и процессы стажировки","Title")
    para(doc,"Концепция продукта и пакет для разработки","Subtitle")
    para(doc,"ORIGINATION   /   10 НОЯБРЯ 2026","Kicker")
    para(doc,"«Хомыч» помогает стажёру понять ближайший шаг, получить корпоративный доступ и подготовиться к первой задаче. Рабочий объём хакатона начинается с уже опубликованного плана. Остальные этапы в документе описывают полный путь стажировки и границы дальнейшей разработки.")
    para(doc,"В первой части собраны контекст, правила прототипа, архитектура и приёмка. Затем идут новые BPMN P00–P14 с условиями запуска и результатом. В конце приведены все 40 шагов листа 3 и идентификаторы элементов BPMN.")
    src=(PROJECT/"product-context.md").read_text(encoding="utf-8")
    context=src[src.index("## 1."):src.index("## Приложение")]
    context=re.sub(r"\| \*\*Схемы\*\* \|[^\n]+",
                   "| **Схемы** | Новые модели P00–P14 встроены в этот документ. Редактируемые .bpmn и таблица соответствия входят в архив. Ранее подготовленные карты и экспорты остаются в папке проекта. |",context)
    markdown(doc,context)
    handoff(doc)
    landscape(doc)
    doc.add_heading("Каталог процессов BPMN",1)
    para(doc,"Диаграммы читаются слева направо. Внутри пула сплошные стрелки показывают порядок действий. Пунктир между пулами обозначает обмен запросом и ответом с тестовым сервисом. Ромб с × показывает выбор одной ветки, ромб с + запускает параллельные действия. Круги обозначают начало и завершение. Иконки у задач различают ручное действие, действие пользователя в интерфейсе и автоматический шаг.")
    para(doc,"Дорожка «Приложение» включает интерфейс и серверные операции. Дорожка «Модель AI» выделяет действия, где модель формулирует текст. Условия веток записаны как бизнес-предикаты. Файлы предназначены для просмотра и редактирования по BPMN 2.0.2 и содержат координаты элементов. Схемы передают в разработку как спецификацию поведения. "+f"[Стандарт OMG]({OMG}).")
    cat=[["Схема","Объём","Событие запуска","Результат"]]
    for m in index["models"]:
        cat.append([m["id"]+" "+m["title"],m["scope"],m["trigger"],m["result"]])
    table(doc,cat,[3.2,2.52,3.4,6.13])
    para(doc,f"Проверено {len(reports)} исходников. Схемы валидны по XSD OMG, открываются в BPMN Modeler без предупреждений, сохраняют изменение после повторного импорта. Покрытие источника — {len(index['rows'])} из 40 строк.")
    for m in index["models"]:
        doc.add_page_break()
        para(doc,m["id"]+"   /   "+m["scope"],"Kicker")
        doc.add_heading(m["title"],1)
        para(doc,"**Запуск.** "+m["trigger"]+".","Diagram Notes")
        para(doc,"**Результат.** "+m["result"],"Diagram Notes")
        picture(doc,image_dir/(m["id"]+".png"),15.15,7.4,m["id"]+" "+m["title"])
        para(doc,"**Приёмка.** "+m["acceptance"],"Diagram Notes")
        for note in m["notes"]:
            para(doc,note,"Diagram Notes")
    doc.add_page_break()
    doc.add_heading("Соответствие шагов листа и BPMN",1)
    para(doc,f"Снимок диапазона A1:F41 на 10 ноября 2026 года. Содержимое шести исходных столбцов сохранено. По идентификаторам в первом столбце можно найти шаг в модели, а в самом элементе BPMN — номер строки, ожидаемый результат и границы участия AI-buddy. [Лист 3 Чек-лист по ролям]({SHEET}).")
    rows=[["Строка и ID BPMN",*checklist[0]]]
    for row,values in enumerate(checklist[1:],2):
        rows.append([str(row)+"\n"+"\n".join(index["rows"][str(row)]),*values])
    table(doc,rows,[1.51,1.85,3.38,1.32,.96,2.75,3.48])
    output.parent.mkdir(parents=True,exist_ok=True)
    doc.save(output)
    print(output)


if __name__=="__main__":
    main()
