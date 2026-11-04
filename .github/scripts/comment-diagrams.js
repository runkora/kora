// Post or update a PR comment listing the diagrams changed in this PR, each
// linking to its rendered SVG. Called from docs-ci.yml via actions/github-script;
// receives that action's { github, context } and reads CHANGED_DIAGRAMS from env.
module.exports = async ({ github, context }) => {
  const marker = '<!-- kora-diagram-preview -->'
  const sha = context.payload.pull_request.head.sha
  const repo = `${context.repo.owner}/${context.repo.repo}`
  const sources = (process.env.CHANGED_DIAGRAMS || '')
    .split('\n').map(s => s.trim()).filter(Boolean)
    // Only repo-relative diagram sources; no traversal, no odd characters.
    .filter(s => /^[A-Za-z0-9._/-]+\.excalidraw$/.test(s) && !s.includes('..'))

  if (sources.length === 0) return

  const rows = sources.map(src => {
    const svg = src.replace(/\.excalidraw$/, '.svg')
    const name = svg.split('/').pop().replace(/\.svg$/, '')
    return `| \`${name}\` | [view rendered](https://github.com/${repo}/blob/${sha}/${svg}) |`
  })

  const body = [
    marker,
    '### Diagrams changed in this PR',
    '',
    '| Diagram | Rendered |',
    '|---|---|',
    ...rows,
    '',
    'The Files changed tab shows a before-and-after image diff for each SVG.',
  ].join('\n')

  const { data: comments } = await github.rest.issues.listComments({
    owner: context.repo.owner,
    repo: context.repo.repo,
    issue_number: context.issue.number,
    per_page: 100,
  })
  const existing = comments.find(c => c.body && c.body.includes(marker))

  if (existing) {
    await github.rest.issues.updateComment({
      owner: context.repo.owner,
      repo: context.repo.repo,
      comment_id: existing.id,
      body,
    })
  } else {
    await github.rest.issues.createComment({
      owner: context.repo.owner,
      repo: context.repo.repo,
      issue_number: context.issue.number,
      body,
    })
  }
}
