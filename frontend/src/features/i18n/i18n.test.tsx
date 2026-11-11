import { describe, it, expect } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { I18nProvider, useI18n } from "./i18n";

function TestConsumer() {
  const { locale, setLocale, t } = useI18n();
  return (
    <div>
      <span data-testid="locale">{locale}</span>
      <span data-testid="title">{t.dashboard.title}</span>
      <button onClick={() => setLocale("sw")}>Set Swahili</button>
      <button onClick={() => setLocale("en")}>Set English</button>
    </div>
  );
}

describe("i18n", () => {
  it("defaults to English and supports switching to Swahili", () => {
    render(
      <I18nProvider>
        <TestConsumer />
      </I18nProvider>,
    );

    expect(screen.getByTestId("locale")).toHaveTextContent("en");
    expect(screen.getByTestId("title")).toHaveTextContent("Welcome to your dashboard");

    fireEvent.click(screen.getByText("Set Swahili"));
    expect(screen.getByTestId("locale")).toHaveTextContent("sw");
    expect(screen.getByTestId("title")).toHaveTextContent("Karibu kwenye dashibodi yako");
  });

  it("provides safe fallback when rendered outside I18nProvider", () => {
    render(<TestConsumer />);
    expect(screen.getByTestId("locale")).toHaveTextContent("en");
    expect(screen.getByTestId("title")).toHaveTextContent("Welcome to your dashboard");
  });
});
