
# Meltica Control UI/UX Feedback

This document summarizes the findings from testing the Meltica Control web UI for strategy module management.

## Create Module

*   **Bug:** The application does not handle syntax errors in the uploaded JavaScript file gracefully. It shows a developer-centric error message instead of a user-friendly one.
*   **Bug:** The application does not validate the metadata of the strategy module. It allows creating a module without the `events` property, which previously caused a crash when trying to view the metadata.
*   **Suggestion:** The UI should provide more guidance to the user on the expected format of the strategy module file, for example by showing a template.
*   **Status (2024-06-02):** Version tag is now a required field. Submitting without a tag raises the inline message “Version tag is required. Provide a semantic version such as v1.2.0.” and no network call is issued. Creating `alpha-integ@v0.0.1` succeeds when the source compiles.
