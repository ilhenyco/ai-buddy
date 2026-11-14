const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const [models, output, qaRoot] = process.argv.slice(2).map(p => path.resolve(p));
const { chromium } = require(path.join(process.env.CODEX_BUNDLED_NODE_MODULES, 'playwright-core'));

async function main() {
  fs.mkdirSync(output, { recursive: true });
  const index = JSON.parse(fs.readFileSync(path.join(models, 'model-index.json'), 'utf8'));
  const browser = await chromium.launch({
    executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe',
    args: ['--no-sandbox', '--allow-file-access-from-files'],
  });
  const page = await browser.newPage({ viewport: { width: 2600, height: 1850 }, deviceScaleFactor: 2 });
  const report = [];
  try {
    await page.goto(pathToFileURL(path.join(qaRoot, 'qa.html')).href);
    for (const record of index.models) {
      const xml = fs.readFileSync(path.join(models, record.id + '.bpmn'), 'utf8');
      const result = await page.evaluate(async ({ xml, record }) => {
        window.modeler?.destroy();
        const modeler = window.modeler = new BpmnJS({
          container: '#canvas',
          textRenderer: {
            defaultStyle: { fontFamily: 'Arial', fontSize: 15, lineHeight: 1.13 },
            externalStyle: { fontSize: 14, lineHeight: 1.13 },
          },
        });
        const { warnings } = await modeler.importXML(xml);
        if (warnings.length) throw new Error(warnings.map(w => w.message).join('\n'));
        const registry = modeler.get('elementRegistry');
        const element = registry.find(e => e.type?.endsWith('Task') || e.type === 'bpmn:SubProcess');
        const id = element.id;
        modeler.get('modeling').updateProperties(element, { name: 'Проверка редактирования' });
        const saved = await modeler.saveXML();
        const second = await modeler.importXML(saved.xml);
        if (second.warnings.length || registry.get(id).businessObject.name !== 'Проверка редактирования') {
          throw new Error('Редактирование не прошло повторный импорт');
        }
        await modeler.importXML(xml);
        for (const key of Object.keys(record.nodes)) {
          if (!registry.get(record.id + '_' + key)) throw new Error('Не отображён узел ' + key);
        }
        for (const edge of record.edges) {
          const e = registry.get(record.id + '_' + edge.key);
          if (e?.source?.id !== record.id + '_' + edge.src || e?.target?.id !== record.id + '_' + edge.dst) {
            throw new Error('Неверные концы потока ' + edge.key);
          }
        }
        const { svg } = await modeler.saveSVG();
        return { svg };
      }, { xml, record });
      const svg = result.svg.slice(result.svg.indexOf('<svg'));
      const viewBox = svg.match(/viewBox="([^"]+)"/)[1].split(/\s+/).map(Number);
      assert.ok(viewBox[2] > 100 && viewBox[3] > 100);
      fs.writeFileSync(path.join(output, record.id + '.svg'), result.svg, 'utf8');
      const render = await browser.newPage({
        viewport: { width: Math.ceil(viewBox[2] + 28), height: Math.ceil(viewBox[3] + 28) },
        deviceScaleFactor: 2,
      });
      await render.setContent(`<!doctype html><meta charset="utf-8">
        <style>html{background:white}body{margin:14px}svg{display:block}</style>${svg}`);
      await render.locator('svg').screenshot({ path: path.join(output, record.id + '.png') });
      await render.close();
      report.push({ id: record.id, nodes: Object.keys(record.nodes).length, flows: record.edges.length,
        importWarnings: 0, editAndReimport: true, viewBox });
      console.log(record.id, 'render, edit and reimport OK', viewBox.join(' '));
    }
  } finally {
    await browser.close();
  }
  fs.writeFileSync(path.join(output, 'render-report.json'), JSON.stringify(report, null, 2));
}

main().catch(error => { console.error(error); process.exitCode = 1; });
