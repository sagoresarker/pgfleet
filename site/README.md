# PgFleet site

The static marketing and docs site for PgFleet — plain HTML, CSS, and a sprinkle of vanilla JS with no build step. To preview it locally, run a static server from this directory and open the printed URL: `cd site && python3 -m http.server 8000`, then visit <http://localhost:8000>. Any change is live on refresh — there is nothing to compile. On push to `main` (or via manual `workflow_dispatch`), the GitHub Actions workflow at `.github/workflows/pages.yml` publishes this `site/` directory to GitHub Pages automatically.
