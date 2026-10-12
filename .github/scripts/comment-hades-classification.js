// Post or update the one PR comment carrying the Hades classification report.
// Called from frontend-ci.yml via actions/github-script; receives that action's
// { github, context } and reads the markdown report from the file named by
// HADES_REPORT_FILE, which `make hades-classify` wrote.
const fs = require('fs')

module.exports = async ({ github, context }) => {
  const marker = '<!-- hades-classification -->'
  const file = process.env.HADES_REPORT_FILE
  if (!file || !fs.existsSync(file)) return
  const body = fs.readFileSync(file, 'utf8')
  if (!body.includes(marker)) return

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
