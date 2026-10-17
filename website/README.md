# Gryph Website

## Development

> Install and setup [PMG](https://github.com/safedep/pmg) before you install dependencies.

Install dependencies:

```
npm install
```

Start development server:

```
npm run dev
```

## Deployment

Build static site:

```
npm run build
```

Deploy to Cloudflare Pages:

```
npx wrangler deploy
```

## Design System

The site uses the Cartridge design system. Find the styles in `src/shared/`:

- `tokens.css` holds the colours, fonts, borders and shadows. Do not write a colour value outside this file. Build each shadow from these tokens.
- `base.css` holds the reset and the global element styles.
- `components.css` holds the shared parts: buttons, cards, windows, tabs, commands, agents and the footer.

Each page imports `components.css` and adds only its own layout. The page styles are in `src/home/home.css` and `src/protect/protect.css`.

The fonts are in `public/fonts/`. Read `public/fonts/LICENSE.txt` before you add or change a font.
