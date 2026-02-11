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
  contents: read
  pull-requests: write

jobs:
  flow2apex-diff:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: octoberswimmer/flow2apex@v0.2.0
        with:
          base-sha: ${{ github.event.pull_request.base.sha }}
          head-sha: ${{ github.event.pull_request.head.sha }}
          version: latest
          diff-format: side-by-side
```

`diff-format` defaults to `unified`; set it to `side-by-side` to render side-by-side output in the PR comment.
When `side-by-side` is enabled, the comment also includes a link to a colored HTML report uploaded as a workflow artifact.
