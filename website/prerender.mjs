// Writes the static HTML of each page into dist, so crawlers see the content without JS.
import { readFile, rm, writeFile } from 'node:fs/promises'

const SSR_DIR = new URL('./dist-ssr/', import.meta.url)
const { pages } = await import(new URL('prerender.js', SSR_DIR))

for (const [file, render] of Object.entries(pages)) {
  const path = new URL(`./dist/${file}`, import.meta.url)
  const html = await readFile(path, 'utf8')
  const root = '<div id="root"></div>'
  if (!html.includes(root)) throw new Error(`${file}: no empty #root`)
  await writeFile(path, html.replace(root, `<div id="root">${render()}</div>`))
}

await rm(SSR_DIR, { recursive: true })
