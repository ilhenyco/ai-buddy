"""Make six shareable product frames from real prototype screenshots.

Does not modify the app, generate UI, or automate a browser. The only crops
remove the prototype-review toolbar at the top of existing captures.
"""
from pathlib import Path
import json
import zipfile

from PIL import Image, ImageDraw, ImageFont, ImageOps
from reportlab.pdfgen import canvas
from reportlab.lib.utils import ImageReader
from pypdf import PdfReader

ROOT = Path(__file__).resolve().parents[2]
SHOTS = ROOT / "frontend/design/screenshots"
OUT = ROOT / "output/product-frames"
FONT_PATH = ROOT / "frontend/public/fonts/Onest-Variable.ttf"
INK, WHITE, GRAY = "#111111", "#FFFFFF", "#555555"
GREEN, BLUE, PURPLE = "#66DD70", "#4CA7F8", "#A582E7"
W, H = 1920, 1080


def font(size, weight=400):
    result = ImageFont.truetype(str(FONT_PATH), size)
    result.set_variation_by_axes([weight])
    return result


def text(draw, xy, value, size, weight=400, fill=INK):
    draw.text(xy, value, font=font(size, weight), fill=fill, anchor="lt")


def wrapped(draw, xy, value, width, size=32, line=46, weight=400, fill=INK):
    x, y = xy
    face = font(size, weight)
    lines = []
    for paragraph in value.split("\n"):
        current = ""
        for word in paragraph.split():
            candidate = (current + " " + word).strip()
            if current and draw.textlength(candidate, font=face) > width:
                lines.append(current)
                current = word
            else:
                current = candidate
        lines.append(current)
    for value_line in lines:
        text(draw, (x, y), value_line, size, weight, fill)
        y += line
    return y


def tag(draw, value, color, x=84, y=62):
    face = font(22, 700)
    width = int(draw.textlength(value, font=face)) + 36
    draw.rectangle((x, y, x + width, y + 44), fill=color, outline=INK, width=2)
    text(draw, (x + 18, y + 11), value, 22, 700, WHITE if color == PURPLE else INK)


def base(index, role, color):
    image = Image.new("RGB", (W, H), WHITE)
    draw = ImageDraw.Draw(image)
    tag(draw, role, color)
    text(draw, (1634, 75), f"ХОМЫЧ  {index:02d}/06", 22, 600)
    draw.line((84, 1022, 1836, 1022), fill=INK, width=2)
    text(draw, (84, 1042), "AI-buddy для стажировки", 20, 500)
    text(draw, (1342, 1042), "Прототип / демонстрационные данные", 20, fill=GRAY)
    return image, draw


def screenshot(image, filename, box, toolbar=60):
    source = Image.open(SHOTS / filename).convert("RGB")
    assert source.height > toolbar
    source = source.crop((0, toolbar, source.width, source.height))
    fit = ImageOps.contain(source, (box[2], box[3]), Image.Resampling.LANCZOS)
    x = box[0] + (box[2] - fit.width) // 2
    y = box[1] + (box[3] - fit.height) // 2
    image.paste(fit, (x, y))
    ImageDraw.Draw(image).rectangle((x - 2, y - 2, x + fit.width + 1, y + fit.height + 1), outline=INK, width=2)
    return [x, y, fit.width, fit.height]


FRAMES = [
    dict(slug="01-intern-checkpoint", role="СТАЖЁР", color=GREEN,
         title="Хомыч пишет\nстажёру сам",
         description="В нужный день в переписке появляется короткий чек-лист. Ссылки ведут к материалам и контактам.",
         note="Следующий этап приходит по сроку. Отметки в чек-листе его не блокируют.",
         source="friends-intern.png"),
    dict(slug="02-private-dialogue", role="ЛИЧНЫЙ ЧАТ", color=GREEN,
         title="Вопросы остаются\nв личном чате",
         description="Стажёр спрашивает о рабочих инструментах. Хомыч объясняет, с чего начать, и даёт ссылку на инструкцию.",
         note="Бадди и руководитель получают плановые сообщения. Личная переписка стажёра остаётся у него.",
         source="friends-dialogue.png"),
    dict(slug="03-buddy-message", role="БАДДИ", color=PURPLE,
         title="Бадди понимает,\nчем помочь",
         description="К тем же пунктам стажёра бот присылает бадди конкретные совместные действия.",
         note="Например: познакомить с командой и договориться, как общаться.",
         source="friends-buddy.png"),
    dict(slug="04-lead-message", role="РУКОВОДИТЕЛЬ", color=BLUE,
         title="Руководитель знает,\nкогда подключиться",
         description="В своём сообщении руководитель видит задачи, которые требуют его участия.",
         note="Например: обсудить ожидания на первой встрече. Важные решения остаются за человеком.",
         source="friends-lead.png"),
    dict(slug="05-plan-builder", role="НАСТРОЙКА СЦЕНАРИЯ", color=GREEN,
         title="Руководитель собирает сценарий",
         description="Выбирает 6 или 9 месяцев, назначает бадди и настраивает сообщения для участников.",
         source="builder-desktop.png", wide=True),
    dict(slug="06-mobile-chat", role="МОБИЛЬНЫЙ ВИД", color=BLUE,
         title="Тот же бот\nна телефоне",
         description="Чек-лист, ссылки и вопросы Хомычу находятся в одной переписке.",
         note="Подсказки над полем ввода помогают начать разговор. Следующая точка указана в конце сообщения.",
         source="intern-mobile-content.png"),
]


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    (ROOT / "output/pdf").mkdir(parents=True, exist_ok=True)
    images, manifest = [], []
    for index, frame in enumerate(FRAMES, 1):
        image, draw = base(index, frame["role"], frame["color"])
        if frame.get("wide"):
            wrapped(draw, (84, 140), frame["title"], 1752, size=60, line=72, weight=500)
            wrapped(draw, (84, 222), frame["description"], 1752, size=28, line=38, fill=GRAY)
            position = screenshot(image, frame["source"], (84, 285, 1752, 706))
        else:
            wrapped(draw, (84, 190), frame["title"], 970, size=68, line=82, weight=500)
            bottom = wrapped(draw, (84, 414), frame["description"], 880, size=34, line=49)
            assert bottom < 636, (index, bottom)
            draw.line((84, 654, 938, 654), fill=INK, width=2)
            text(draw, (84, 690), "КАК ЭТО РАБОТАЕТ", 21, 700, GRAY)
            end = wrapped(draw, (84, 740), frame["note"], 850, size=30, line=44)
            assert end < 1000
            position = screenshot(image, frame["source"], (1084, 145, 752, 842))
        target = OUT / (frame["slug"] + ".png")
        image.save(target, optimize=True)
        images.append(image)
        manifest.append({"frame": index, "file": target.name, "source": frame["source"],
                         "title": frame["title"].replace("\n", " "),
                         "screenshot_bounds": position})

    pdf_path = ROOT / "output/pdf/homych-product-frames.pdf"
    pdf = canvas.Canvas(str(pdf_path), pagesize=(960, 540))
    pdf.setTitle("Хомыч: шесть кадров продукта")
    pdf.setAuthor("Команда AI-buddy")
    pdf.setSubject("Кадры работающего интерфейсного прототипа. Дизайн команды Origination, Onest.")
    for image in images:
        pdf.drawImage(ImageReader(image), 0, 0, width=960, height=540)
        pdf.showPage()
    pdf.save()
    assert len(PdfReader(pdf_path).pages) == 6

    overview = Image.new("RGB", (1920, 1620), WHITE)
    for index, image in enumerate(images):
        overview.paste(image.resize((960, 540), Image.Resampling.LANCZOS),
                       ((index % 2) * 960, (index // 2) * 540))
    overview.save(ROOT / "output/homych-product-frames-overview.png", optimize=True)
    (OUT / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2), encoding="utf-8")
    zip_path = ROOT / "output/homych-product-frames.zip"
    with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as archive:
        for frame in FRAMES:
            target = OUT / (frame["slug"] + ".png")
            archive.write(target, target.name)
    with zipfile.ZipFile(zip_path) as archive:
        assert archive.testzip() is None
        assert len(archive.namelist()) == 6
    print(json.dumps({"pdf_pages": 6, "frames": len(images), "zip_bytes": zip_path.stat().st_size}))


if __name__ == "__main__":
    main()
