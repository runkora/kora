# Deploying kora Documentation & Presentation to Vercel

## Quick Start

### Option 1: Deploy from GitHub (Recommended)

1. Go to [vercel.com](https://vercel.com) and sign in
2. Click "Add New..." → "Project"
3. Select your GitHub repository (`runkora/kora`)
4. Click "Import"
5. Vercel will automatically detect the `vercel.json` configuration
6. Click "Deploy"

**That's it!** Your presentation will be live at a URL like `kora.vercel.app`

### Option 2: Deploy from CLI

```bash
# Install Vercel CLI
npm i -g vercel

# Deploy from project directory
cd /path/to/kora
vercel

# Follow the prompts
# First deployment will ask for project setup
# Subsequent deployments just run: vercel
```

### Option 3: Deploy from Git Push

```bash
# Push to the kora-doc branch
git push origin kora-doc

# Vercel will automatically deploy if connected to GitHub
```

## Project Structure

```
kora/
├── public/                    # Served by Vercel
│   ├── index.html            # Landing page
│   ├── presentation.html      # 17-slide presentation
│   └── docs/
│       ├── doc.md            # Full documentation
│       └── diagrams.md        # Architecture diagrams
├── vercel.json               # Vercel configuration
└── .vercelignore             # Files to ignore
```

## Configuration

The `vercel.json` file configures:
- **Static file serving** from `public/` folder
- **Routes** for presentation and documentation
- **GitHub integration** for automatic deployments

```json
{
  "name": "kora-presentation",
  "version": 2,
  "builds": [
    {
      "src": "public/**",
      "use": "@vercel/static"
    }
  ],
  "routes": [
    {
      "src": "/presentation/?$",
      "dest": "/presentation.html"
    },
    {
      "src": "/docs/?$",
      "dest": "/docs.html"
    }
  ]
}
```

## What Gets Deployed

✅ `public/index.html` — Landing page with links
✅ `public/presentation.html` — Interactive 17-slide presentation
✅ `public/docs/doc.md` — Complete documentation
✅ `public/docs/diagrams.md` — Architecture diagrams

❌ `.go` files, backend code, etc. (ignored via `.vercelignore`)

## URLs After Deployment

- **Home:** `https://your-project.vercel.app`
- **Presentation:** `https://your-project.vercel.app/presentation.html`
- **Docs:** `https://your-project.vercel.app/docs/doc.md`
- **Diagrams:** `https://your-project.vercel.app/docs/diagrams.md`

## Custom Domain

To use a custom domain:

1. Go to Project Settings on Vercel
2. Navigate to "Domains"
3. Add your custom domain
4. Update DNS records as instructed

Example: `kora.dev` → points to Vercel deployment

## Automatic Deployments

With GitHub integration enabled:
- Every push to `kora-doc` branch automatically deploys
- Every commit shows deployment status on GitHub
- Preview deployments for PRs (if enabled)
- Production deployment when merged to main

## Environment Variables

If needed in the future, add to Vercel:

1. Go to Project Settings → Environment Variables
2. Add key-value pairs
3. They're injected at build/runtime

Currently not needed (static site).

## Troubleshooting

### Site shows 404

- Check that `public/` folder exists
- Verify files are in `public/` folder
- Check Vercel build logs

### Presentation doesn't render

- Make sure `public/presentation.html` is deployed
- Check browser console for errors
- Verify Mermaid CDN is accessible

### Routes not working

- Verify `vercel.json` syntax
- Redeploy after updating `vercel.json`
- Check route patterns match your URLs

### Build fails

- Check `.vercelignore` isn't hiding necessary files
- Verify `public/` folder has content
- Review Vercel build logs for errors

## Monitoring

After deployment, you can:
- View analytics at vercel.com dashboard
- Check deployment logs
- Monitor page performance
- Set up alerts

## Rollback

To rollback to previous version:

1. Go to Vercel dashboard → Project
2. Click "Deployments"
3. Find the version you want
4. Click "..." → "Promote to Production"

## File Size & Performance

Deployment size:
- `index.html` — ~7 KB
- `presentation.html` — ~28 KB
- `doc.md` — ~48 KB
- `diagrams.md` — ~28 KB
- **Total:** ~111 KB (very small, fast delivery)

Vercel automatically:
- Compresses files
- Caches at CDN edges
- Serves from nearest location globally

## Next Steps

1. Deploy to Vercel
2. Share the URL with your team
3. Add to README.md
4. Share in documentation

Example:

```markdown
## 📊 Documentation

- [Interactive Presentation](https://kora.vercel.app) — 17 slides on kora architecture
- [Full Documentation](https://kora.vercel.app/docs/doc.md) — Complete first-principles guide
- [Architecture Diagrams](https://kora.vercel.app/docs/diagrams.md) — 15 Mermaid diagrams
```
