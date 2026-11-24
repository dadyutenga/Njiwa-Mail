import posthog from "posthog-js";

const posthogKey = import.meta.env.VITE_POSTHOG_KEY;
const posthogHost = import.meta.env.VITE_POSTHOG_HOST;

// Analytics are optional: a self-hosted dashboard without a PostHog key runs with them
// off (every posthog call is then a no-op). Never fail the app or its tests over it.
if (!posthogKey || !posthogHost) {
  if (import.meta.env.DEV && import.meta.env.MODE !== "test") {
    const missingVariable = posthogKey ? "VITE_POSTHOG_HOST" : "VITE_POSTHOG_KEY";
    console.info(`PostHog analytics are off: ${missingVariable} is not set.`);
  }
} else {
  posthog.init(posthogKey, {
    api_host: posthogHost,
    defaults: "2026-08-30",
    capture_exceptions: {
      capture_unhandled_errors: true,
      capture_unhandled_rejections: true,
      capture_console_errors: false,
    },
    logs: {
      serviceName: "njiwa-mail-dashboard",
      environment: import.meta.env.MODE,
    },
  });
}

export default posthog;
