import fs from 'node:fs'
import { launch, openDesktop } from './desktop.mjs'

const OUT = '../shots/raw'
fs.mkdirSync(OUT, { recursive: true })

const W = 1440
const H = 900
const only = process.argv.slice(2)

const settle = (page, ms = 900) => page.waitForTimeout(ms)

const toDeck = async (page, index) => {
  await page.keyboard.press('Escape')
  await settle(page, 500)
  const back = 4 - index
  for (let i = 0; i < back; i++) {
    await page.keyboard.press('Control+ArrowLeft')
    await settle(page, 140)
  }
  await settle(page, 1000)
}

const inCenter = async (locator) => {
  const count = await locator.count()
  for (let i = 0; i < count; i++) {
    const box = await locator.nth(i).boundingBox()
    if (box && box.x > 600 && box.x < 1100 && box.y < 180 && box.width > 0) return locator.nth(i)
  }
  return null
}

const views = async (page, label) => {
  const trigger = await inCenter(page.getByRole('button', { name: 'Views' }))
  await trigger.click()
  await settle(page, 800)
  const pills = page.locator('button.ma-action').filter({ hasText: label })
  const target = await inCenter(pills)
  await target.click()
  await settle(page, 2400)
}

const shots = {
  'deck-hero': {
    wallpaper: 'builtin-4',
    run: async (page) => {
      await toDeck(page, 0)
      await page.getByText('Worked for 52s').first().click()
      await settle(page, 1000)
    },
  },
  'deck-simple': {
    wallpaper: 'builtin-4',
    run: async (page) => {
      await toDeck(page, 0)
    },
  },
  grid: {
    wallpaper: 'builtin-4',
    run: async (page) => {
      await page.keyboard.press('Escape')
      await settle(page, 500)
      await page.keyboard.press('Control+ArrowUp')
      await settle(page, 1500)
    },
  },
  plan: {
    wallpaper: 'builtin-4',
    run: async (page) => {
      await page.keyboard.press('Escape')
      await settle(page, 400)
      await page.keyboard.press('Control+ArrowUp')
      await settle(page, 300)
      await page.keyboard.press('Control+ArrowUp')
      await settle(page, 1600)
    },
  },
  git: {
    wallpaper: 'builtin-4',
    run: async (page) => {
      await toDeck(page, 0)
      await views(page, /^Changes/)
    },
  },
  preview: {
    wallpaper: 'builtin-4',
    run: async (page) => {
      await toDeck(page, 0)
      await views(page, /^Preview/)
      await settle(page, 1500)
    },
  },
  tasks: {
    wallpaper: 'builtin-4',
    run: async (page) => {
      await toDeck(page, 1)
      await views(page, /^Tasks/)
    },
  },
  question: {
    wallpaper: 'builtin-4',
    run: async (page) => {
      await toDeck(page, 2)
    },
  },
  models: {
    wallpaper: 'builtin-4',
    run: async (page) => {
      await toDeck(page, 0)
      await page.mouse.click(440, 849)
      await settle(page, 1000)
    },
  },
  usage: {
    wallpaper: 'builtin-4',
    run: async (page) => {
      await toDeck(page, 0)
      await page.mouse.click(829, 30)
      await settle(page, 1200)
    },
  },
  'settings-panel': {
    wallpaper: 'builtin-2',
    run: async (page) => {
      await toDeck(page, 0)
      await page.mouse.click(46, 450)
      await settle(page, 1200)
    },
  },
  'settings-phone': {
    wallpaper: 'builtin-4',
    run: async (page) => {
      await toDeck(page, 0)
      await page.mouse.click(46, 450)
      await settle(page, 900)
      await page.mouse.click(275, 655)
      await settle(page, 1300)
      await page.mouse.click(369, 323)
      await settle(page, 1300)
    },
  },
  'settings-providers': {
    wallpaper: 'builtin-4',
    run: async (page) => {
      await toDeck(page, 0)
      await page.mouse.click(46, 450)
      await settle(page, 900)
      await page.mouse.click(275, 655)
      await settle(page, 1300)
      await page.mouse.click(356, 247)
      await settle(page, 1300)
    },
  },
  'deck-glass': {
    wallpaper: 'builtin-3',
    storage: { catalyst_theme_id: 'glass' },
    run: async (page) => {
      await toDeck(page, 0)
    },
  },
  'deck-light': {
    wallpaper: 'builtin-2',
    storage: { catalyst_theme_id: 'light' },
    run: async (page) => {
      await toDeck(page, 0)
    },
  },
  'wall-pink': {
    wallpaper: 'builtin-2',
    run: async (page) => {
      await toDeck(page, 0)
    },
  },
  'wall-sierra': {
    wallpaper: 'builtin-1',
    run: async (page) => {
      await toDeck(page, 0)
    },
  },
  'wall-day': {
    wallpaper: 'builtin-5',
    storage: { catalyst_theme_id: 'light' },
    run: async (page) => {
      await toDeck(page, 0)
    },
  },
  'wall-silk': {
    wallpaper: 'builtin-6',
    storage: { catalyst_theme_id: 'glass' },
    run: async (page) => {
      await toDeck(page, 0)
    },
  },
  'wall-grid-pink': {
    wallpaper: 'builtin-2',
    run: async (page) => {
      await page.keyboard.press('Escape')
      await settle(page, 500)
      await page.keyboard.press('Control+ArrowUp')
      await settle(page, 1500)
    },
  },
}

const browser = await launch()
for (const [name, def] of Object.entries(shots)) {
  if (only.length && !only.includes(name)) continue
  const { ctx, page } = await openDesktop(browser, { width: W, height: H, scale: 2, storage: { 'composer:wallpaper': def.wallpaper, ...(def.storage || {}) } })
  await page.waitForTimeout(5600)
  await def.run(page)
  await page.screenshot({ path: `${OUT}/desktop-${name}.png` })
  console.log('shot', name)
  await ctx.close()
}
await browser.close()

