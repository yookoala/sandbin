# Agent Notes

1. Always plan before action.
2. Always make plans for implementer and reviewer separately. Each should have a TODO list of their own.
3. Always spawn a separated implementer and reviewer subagent for a plan.
4. Remember to run proper test at the end of implementation or review to make sure no regression. Fix all
   test problem if encountered.
5. Always consider to make atomic commit when working.
6. Check folder structure before start working.
7. Only read relevant documentations in "docs/". Use grob, grep or string search to find
   relevant files. Ignore unrelated documentations to preserve context window.


## Folder Structure

1. "docs/plans": All plans are documented into the subfolder.
2. "docs/decisions": All made decisions memory are documented into the subfolder here.
