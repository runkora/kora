// The trigger kora's own repository runs on, as trigger-create and the story
// walkthroughs create it: a comment that mentions @kora-agent starts a run, and
// kora chooses the agent. Its name, source and prompt are copied from
// production.
export const TRIGGER = "Kora Agent (mentioned)";
export const SOURCE = "runkora-github";

// Its prompt opens with the instruction, typed, and continues with the
// delivery's fields, pasted as the block it is in production.
export const INSTRUCTION =
  "{{ comment.user.login }} mentioned @kora-agent in a comment. Do what the comment asks, within your role.";
export const FIELDS = `

Repository: {{ repository.full_name }}
Number: #{{ issue.number }}
Title: {{ issue.title }}
State: {{ issue.state }}
Link: {{ issue.html_url }}
Pull request link: {{ issue.pull_request.html_url }}
Comment: {{ comment.body }}
Comment link: {{ comment.html_url }}

If Pull request link is empty, this is an issue. Otherwise it is a pull request with the same number. Post your answer as a comment on the issue or pull request. Do not answer only in the run.`;
