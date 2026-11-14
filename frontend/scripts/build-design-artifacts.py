"""Compose local QA evidence and a portable handoff from browser captures.

This script does not capture or automate a browser. Screenshots must already
have been saved from the working prototype through the UI tool.
"""
from pathlib import Path
import json
import shutil
import zipfile
from PIL import Image, ImageDraw, ImageFont, ImageOps

FRONT = Path(__file__).resolve().parents[1]
ROOT = FRONT.parent
DESIGN = FRONT / "design"
SHOTS = DESIGN / "screenshots"
FONT = ImageFont.truetype("C:/Windows/Fonts/arial.ttf", 21)
SMALL = ImageFont.truetype("C:/Windows/Fonts/arial.ttf", 16)


def panel(image, label, width=960, height=600):
    canvas = Image.new("RGB", (width, height + 52), "#f3f3f3")
    draw = ImageDraw.Draw(canvas)
    draw.text((16, 15), label, font=FONT, fill="#171717")
    fit = ImageOps.contain(image.convert("RGB"), (width, height), Image.Resampling.LANCZOS)
    canvas.paste(fit, ((width - fit.width) // 2, 52 + (height - fit.height) // 2))
    return canvas


def compare():
    reference = DESIGN / "reference"
    old_capture = Path("C:/Users/Redmi/AppData/Local/Temp/ai-buddy-frontend-reference/components-browser.png")
    if old_capture.exists():
        shutil.copy2(old_capture, reference / "components-browser.png")
    guide = Image.open(reference / "origination/team-design-system.png").crop((100,100,1380,1200))
    light = Image.open(SHOTS / "intern-desktop.png")
    dark = Image.open(SHOTS / "intern-dark.png")
    role = Image.open(SHOTS / "role-preview.png")
    components = Image.open(SHOTS / "components.png")
    full = Image.new("RGB", (1920, 1304), "white")
    full.paste(panel(guide, "Источник: дизайн-код Origination / Figma"), (0, 0))
    full.paste(panel(light, "Хомыч: светлая тема · 1280 × 720"), (960, 0))
    full.paste(panel(guide.crop((668,430,1225,800)), "Источник: кнопки, теги и отметки"), (0, 652))
    full.paste(panel(role, "Хомыч: окно предпросмотра"), (960, 652))
    full.save(DESIGN / "comparison-full.png")
    focused = Image.new("RGB", (1920, 1304), "white")
    focused.paste(panel(guide.crop((56,430,600,710)), "Источник: типографика Onest"), (0, 0))
    focused.paste(panel(light.crop((250,270,1000,565)), "Хомыч: Onest и плановое сообщение"), (960, 0))
    focused.paste(panel(guide.crop((668,430,1225,790)), "Источник: общие компоненты"), (0, 652))
    focused.paste(panel(components.crop((36,258,1240,550)), "Хомыч: кнопки и поля"), (960, 652))
    focused.save(DESIGN / "comparison-details.png")

    overview = Image.new("RGB", (1920, 1304), "white")
    entries = [("intern-desktop.png", "Стажёр · сообщение периода"),
               ("builder-desktop.png", "Руководитель · конструктор"),
               ("buddy-desktop.png", "Бадди · сообщение о помощи"),
               ("knowledge-desktop.png", "Материалы · вариант управления знаниями")]
    for i, (name, label) in enumerate(entries):
        overview.paste(panel(Image.open(SHOTS / name), label), ((i % 2) * 960, (i // 2) * 652))
    overview.save(ROOT / "output" / "ai-buddy-frontend-overview.png")

    mobile_raw = Image.open(SHOTS / "intern-mobile.png")
    sheet_raw = Image.open(SHOTS / "mobile-sheet.png")
    mobile_left = (mobile_raw.width-414)//2
    sheet_left = (sheet_raw.width-414)//2
    mobile = mobile_raw.crop((mobile_left,0,mobile_left+414,mobile_raw.height))
    sheet = sheet_raw.crop((sheet_left,0,sheet_left+414,sheet_raw.height))
    mobile.save(SHOTS / "intern-mobile-content.png")
    sheet.save(SHOTS / "mobile-sheet-content.png")

    # A browsable static image index complements the running application.
    cards = "\n".join(f'<figure><a href="screenshots/{p.name}"><img loading="lazy" src="screenshots/{p.name}" alt="{p.stem}"></a><figcaption>{p.stem}</figcaption></figure>' for p in sorted(SHOTS.glob("*.png")))
    (DESIGN / "index.html").write_text('''<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Хомыч · экраны</title><style>body{font-family:Arial,sans-serif;color:#171717;margin:32px;background:#fafafa}h1{font-size:32px}p{max-width:800px;line-height:1.6}main{display:grid;grid-template-columns:repeat(auto-fit,minmax(320px,1fr));gap:24px}figure{margin:0;background:white;border:1px solid #e5e5e5;padding:12px}img{width:100%;height:300px;object-fit:contain}figcaption{padding:12px 0 0;font-size:14px}a{color:#007ac2}</style><h1>Хомыч · экраны прототипа</h1><p>Снимки из работающего браузера. Интерактивный дизайн находится в приложении React. Визуальная основа — <a href="https://www.figma.com/design/pDmYotOu0D7yKMmhoN9UtG/Avito?node-id=10-1289">дизайн-код команды Origination</a>. Макет не подключён к корпоративным системам и модели. Предложения HR и управления знаниями отмечены в интерфейсе.</p><main>''' + cards + '</main></html>', encoding="utf-8")
    print(json.dumps({"screenshots": len(list(SHOTS.glob('*.png'))), "comparison": str(DESIGN / 'comparison-full.png')}, ensure_ascii=False))


def package():
    output = ROOT / "output" / "ai-buddy-frontend.zip"
    forbidden = {"node_modules", ".git", ".cache"}
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, strict_timestamps=False) as archive:
        for path in sorted(FRONT.rglob("*")):
            if not path.is_file() or any(part in forbidden for part in path.relative_to(FRONT).parts):
                continue
            if path.name.startswith(".env") or path.name == ".npmrc" or path.suffix == ".log":
                continue
            archive.write(path, "frontend/" + path.relative_to(FRONT).as_posix())
        archive.write(ROOT / "docs/product/functional-requirements.md", "context/functional-requirements.md")
        archive.write(ROOT / "docs/product/decisions/2026-11-12-proactive-bot.md", "context/2026-11-12-proactive-bot.md")
        archive.write(ROOT / "docs/product/decisions/2026-11-12-scenario-library.md", "context/2026-11-12-scenario-library.md")
        archive.write(ROOT / "docs/product/decisions/2026-11-12-stage-suggestions.md", "context/2026-11-12-stage-suggestions.md")
        for path in sorted((ROOT / "output/bpmn-v3/models").iterdir()):
            if path.is_file():
                archive.write(path, "context/bpmn-v3/" + path.name)
        archive.write(ROOT / "output/ai-buddy-bpmn-v3.docx", "context/ai-buddy-bpmn-v3.docx")
    with zipfile.ZipFile(output) as archive:
        assert archive.testzip() is None
        names = archive.namelist()
        for required in ["frontend/src/App.tsx", "frontend/dist/client/index.html", "frontend/design-qa.md", "context/bpmn-v3/P17.bpmn"]:
            assert required in names, required
        assert not any("node_modules/" in name or name.endswith(".env") for name in names)
    print(json.dumps({"archive": str(output), "bytes": output.stat().st_size, "entries": len(names)}, ensure_ascii=False))


if __name__ == "__main__":
    import sys
    if "--package" in sys.argv:
        package()
    else:
        compare()
