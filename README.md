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
