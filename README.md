# flow2apex

`flow2apex` converts Salesforce Flow metadata (`.flow-meta.xml`) into Apex code
for review and refinement.

**Status:** This command is experimental. Feedback and bug reports are welcome.

## Install

Download a release archive and place `flow2apex` on your `PATH`.

## Usage

```bash
flow2apex path/to/MyFlow.flow-meta.xml
flow2apex path/to/MyFlow.flow-meta.xml -o src/triggers/MyFlow.trigger
flow2apex path/to/MyScheduledFlow.flow-meta.xml -d src/
flow2apex path/to/MySubflow.flow-meta.xml -d src/
```

## Notes

- Record-triggered flows generate an Apex trigger.
- Scheduled flows generate a trigger and Queueable class (requires `-d`).
- Auto-launched sub-flows generate an invocable Apex class.

## Reusable GitHub Action

This repository also provides a reusable composite action (`action.yml`) for pull request flow diff comments.
By default, it installs the latest published `flow2apex` release.

Example usage in another repository:

```yaml
name: Flow2Apex Diff

on:
  pull_request:
    types: [opened, reopened, synchronize]

permissions:
  contents: write
  pull-requests: write

jobs:
  flow2apex-diff:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: octoberswimmer/flow2apex@main
        with:
          base-sha: ${{ github.event.pull_request.base.sha }}
          head-sha: ${{ github.event.pull_request.head.sha }}
          version: latest
          diff-format: side-by-side
          vitrine-url: https://vitrine.octoberswimmer.com/
          commit-generated-apex-path: .github/flow2apex-generated
```

`diff-format` defaults to `unified`; set it to `side-by-side` to render side-by-side output in the PR comment.
When `side-by-side` is enabled, the comment includes a link to a colored HTML report.
`vitrine-url` defaults to `https://vitrine.octoberswimmer.com/`; override it only if you host Vitrine elsewhere.
`commit-generated-apex-path` is optional; when set, the action writes generated Apex files into that repository-relative directory, creates a commit if files changed, and pushes it to the PR branch.
For most teams, this should point to a review-only directory (for example `.github/flow2apex-generated`) rather than `force-app` deployment paths.
When enabled, generation runs across tracked flow files at `HEAD`, so missing generated Apex files can be backfilled and committed even if a given push did not modify the corresponding flow file.

If you set `commit-generated-apex-path`:

- workflow `permissions.contents` must be `write`
- `actions/checkout` must keep credentials (default behavior) so the push can authenticate

To keep generated Apex committed on branch updates (including fork branches) and support manual backfill runs, run the action on `push`, `pull_request`, and `workflow_dispatch`:

```yaml
name: Flow2Apex Diff

on:
  push:
    branches:
      - '**'
  pull_request:
    types: [opened, reopened, synchronize]
  workflow_dispatch:

permissions:
  contents: write
  pull-requests: write

jobs:
  flow2apex-diff:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: octoberswimmer/flow2apex@main
        with:
          base-sha: ${{ github.event_name == 'pull_request' && github.event.pull_request.base.sha || github.event_name == 'push' && github.event.before || github.sha }}
          head-sha: ${{ github.event_name == 'pull_request' && github.event.pull_request.head.sha || github.sha }}
          version: latest
          diff-format: side-by-side
          vitrine-url: https://vitrine.octoberswimmer.com/
          commit-generated-apex-path: .github/flow2apex-generated
          post-comment: ${{ github.event_name == 'pull_request' && 'true' || 'false' }}
```

With this pattern, branch update runs commit generated Apex as needed, PR runs usually have no generated Apex changes left to commit, and manual runs against a selected branch can backfill any missing generated Apex files.
