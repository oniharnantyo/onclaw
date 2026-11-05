/**
 * SecretGate tests (add-browser-login-handover task 4.1, spec chat-approvals
 * "Secret card parks and clears"): the masked render (password field, digit
 * boxes per the requested length), the save-to-vault checkbox only when the
 * backend passed a username, the submit delegation — the assembled text and
 * save flag reach `client.chat.resolveInput` and the local value is cleared
 * regardless of the outcome — the fixed reason error surfacing, and the
 * resolved receipt rendering with no trace of any value (there is none: the
 * card never holds one after submit, by construction).
 */
import { act } from "react";
import { renderUI } from "../../../__test__/test-utils";
import { mockedSurface } from "../../../../features/chat/__test__/test-utils";
import { fireEvent, screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type { Client } from "../../../../lib/client";
import type { InputApproval } from "../../../../features/chat/runtime/mapping";
import { useChatStore } from "../../../../features/chat/store";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: only resolveInput and cancelInput are reachable
 * here. */
type MockedClient = Client & {
  chat: { resolveInput: MockFn; cancelInput: MockFn };
};

function makeClient(): MockedClient {
  // SAFETY: the literal covers the surface this suite drives; the
  // Client-typed intersection defeats the structural check, so the
  // conversion funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>({
    chat: {
      resolveInput: vi.fn(async (): Promise<void> => undefined),
      cancelInput: vi.fn(async (): Promise<void> => undefined),
    },
  });
}

vi.mock("../../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../../lib/client";
import { SecretGate } from "../secret-gate";

// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedResolveInput = (client as MockedClient).chat.resolveInput;
const mockedCancelInput = (client as MockedClient).chat.cancelInput;

function secretApproval(
  extra: Partial<InputApproval> = {}
): InputApproval {
  return {
    id: "int-1",
    prompt: "Sign in at https://example.com",
    display: "text",
    attention: "secret",
    method: "password",
    ...extra,
  };
}

beforeEach(async () => {
  vi.clearAllMocks();

  await act(async () => {
    useChatStore.setState({
      activeAgentID: "a1",
      activeSessionID: "s1",
      entries: [],
      busy: true,
    });
  });
});

describe("the masked secret gate (task 4.1)", () => {
  it("renders a masked password input with autocomplete off", async () => {
    const { unmount } = await renderUI(<SecretGate approval={secretApproval()} />);

    const input = screen.getByTestId("secret-gate-password") as HTMLInputElement;

    expect(input.type).toBe("password");
    expect(input.getAttribute("autocomplete")).toBe("off");

    await unmount();
  });

  it("renders one digit box per requested OTP digit", async () => {
    const { unmount } = await renderUI(
      <SecretGate
        approval={secretApproval({ method: "otp", digits: 6 })}
      />
    );

    for (let i = 0; i < 6; i += 1) {
      expect(screen.getByTestId(`secret-gate-otp-${i}`)).toBeTruthy();
    }

    expect(screen.queryByTestId("secret-gate-password")).toBeNull();

    await unmount();
  });

  it("offers save-to-vault only for a password with a username", async () => {
    const withUser = await renderUI(
      <SecretGate
        approval={secretApproval({ username: "ada@example.com" })}
      />
    );

    expect(screen.getByTestId("save-vault-checkbox")).toBeTruthy();

    await withUser.unmount();

    const noUser = await renderUI(<SecretGate approval={secretApproval()} />);

    expect(screen.queryByTestId("save-vault-checkbox")).toBeNull();

    await noUser.unmount();

    const otp = await renderUI(
      <SecretGate
        approval={secretApproval({ method: "otp", digits: 6, username: "ada" })}
      />
    );

    expect(screen.queryByTestId("save-vault-checkbox")).toBeNull();

    await otp.unmount();
  });

  it("submits the typed password through resolveInput and clears the value", async () => {
    const { unmount } = await renderUI(<SecretGate approval={secretApproval()} />);

    await act(async () => {
      fireEvent.change(screen.getByTestId("secret-gate-password"), {
        target: { value: "hunter2" },
      });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId("secret-gate-submit"));
    });

    expect(mockedResolveInput).toHaveBeenCalledTimes(1);
    expect(mockedResolveInput).toHaveBeenCalledWith("s1", "int-1", "hunter2", "", false);

    // Cleared regardless of outcome: the value's lifetime is one submission.
    const input = screen.getByTestId("secret-gate-password") as HTMLInputElement;

    expect(input.value).toBe("");

    await unmount();
  });

  it("submits concatenated OTP digits and the save flag when checked", async () => {
    const { unmount } = await renderUI(
      <SecretGate
        approval={secretApproval({ username: "ada@example.com" })}
      />
    );

    await act(async () => {
      fireEvent.change(screen.getByTestId("secret-gate-password"), {
        target: { value: "pw1" },
      });
      fireEvent.click(screen.getByTestId("save-vault-checkbox"));
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId("secret-gate-submit"));
    });

    expect(mockedResolveInput).toHaveBeenCalledWith("s1", "int-1", "pw1", "", true);

    await unmount();
  });

  it("cancels through cancelInput and clears the typed values", async () => {
    const { unmount } = await renderUI(
      <SecretGate approval={secretApproval({ identifier: true })} />
    );

    await act(async () => {
      fireEvent.change(screen.getByTestId("secret-gate-identifier"), {
        target: { value: "sam@corp.example" },
      });
      fireEvent.change(screen.getByTestId("secret-gate-password"), {
        target: { value: "hunter2" },
      });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId("secret-gate-cancel"));
    });

    expect(mockedCancelInput).toHaveBeenCalledTimes(1);
    expect(mockedCancelInput).toHaveBeenCalledWith("s1", "int-1");
    expect(mockedResolveInput).not.toHaveBeenCalled();

    // The typed values die with the request: nothing is delivered.
    expect((screen.getByTestId("secret-gate-identifier") as HTMLInputElement).value).toBe("");
    expect((screen.getByTestId("secret-gate-password") as HTMLInputElement).value).toBe("");

    await unmount();
  });

  it("submits the concatenated OTP boxes as the assembled text", async () => {
    const { unmount } = await renderUI(
      <SecretGate approval={secretApproval({ method: "otp", digits: 6 })} />
    );

    await act(async () => {
      fireEvent.change(screen.getByTestId("secret-gate-otp-0"), {
        target: { value: "4" },
      });
      fireEvent.change(screen.getByTestId("secret-gate-otp-1"), {
        target: { value: "2" },
      });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId("secret-gate-submit"));
    });

    expect(mockedResolveInput).toHaveBeenCalledWith("s1", "int-1", "42", "", false);

    // The boxes are cleared too (clear-on-submit).
    const first = screen.getByTestId("secret-gate-otp-0") as HTMLInputElement;

    expect(first.value).toBe("");

    await unmount();
  });

  it("surfaces the backend's fixed reason and re-arms after a failure", async () => {
    mockedResolveInput.mockRejectedValueOnce(new Error("Submission failed"));

    const { unmount } = await renderUI(<SecretGate approval={secretApproval()} />);

    await act(async () => {
      fireEvent.change(screen.getByTestId("secret-gate-password"), {
        target: { value: "nope" },
      });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId("secret-gate-submit"));
    });

    expect(screen.getByTestId("secret-gate-error").textContent).toBe(
      "Submission failed"
    );

    // Re-armed: the gate answers a fresh (re-typed) submission.
    const submit = screen.getByTestId(
      "secret-gate-submit"
    ) as HTMLButtonElement;

    expect(submit.disabled).toBe(false);

    await unmount();
  });

  it("renders the receipt on a resolved approval with no trace of a value", async () => {
    const { unmount } = await renderUI(
      <SecretGate
        approval={secretApproval({
          receipt: { outcome: "completed", length: 7, savedLoginId: "login-9" },
        })}
      />
    );

    expect(screen.getByTestId("secret-gate-receipt").textContent).toContain(
      "Filled ✓ (saved as login `login-9`)"
    );
    expect(screen.queryByTestId("secret-gate-password")).toBeNull();
    expect(screen.queryByTestId("secret-gate-submit")).toBeNull();

    await unmount();
  });

  it("renders cancelled and expired receipts with their own wording", async () => {
    const cancelled = await renderUI(
      <SecretGate
        approval={secretApproval({ receipt: { outcome: "cancelled" } })}
      />
    );

    expect(screen.getByTestId("secret-gate-receipt").textContent).toBe(
      "Cancelled"
    );

    await cancelled.unmount();

    const expired = await renderUI(
      <SecretGate
        approval={secretApproval({ receipt: { outcome: "expired" } })}
      />
    );

    expect(screen.getByTestId("secret-gate-receipt").textContent).toBe(
      "Expired — the agent can re-request"
    );

    await expired.unmount();
  });

  it("carries no value anywhere in its resolved descriptor, by construction", () => {
    const approval: InputApproval = secretApproval({
      receipt: { outcome: "completed", length: 7, savedLoginId: "login-9" },
    });

    const encoded = JSON.stringify(approval);

    expect(Object.keys(approval)).not.toContain("value");
    expect(Object.keys(approval)).not.toContain("text");
    expect(encoded).not.toContain("hunter2");
  });
});

describe("the identifier box (username or email)", () => {
  it("renders an identifier box only when the request declared one, and submits both values", async () => {
    const { unmount } = await renderUI(
      <SecretGate approval={secretApproval({ identifier: true })} />
    );

    // The identifier box exists; the password field keeps its testid.
    const identifier = screen.getByTestId("secret-gate-identifier") as HTMLInputElement;
    expect((identifier as HTMLInputElement).type).toBe("text");
    screen.getByTestId("secret-gate-password");

    await act(async () => {
      fireEvent.change(identifier, { target: { value: "sam@corp.example" } });
      fireEvent.change(screen.getByTestId("secret-gate-password"), {
        target: { value: "hunter2" },
      });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId("secret-gate-submit"));
    });

    expect(mockedResolveInput).toHaveBeenCalledTimes(1);
    expect(mockedResolveInput).toHaveBeenCalledWith(
      "s1",
      "int-1",
      "hunter2",
      "sam@corp.example",
      false
    );

    // Cleared regardless of outcome: the identifier's lifetime is one
    // submission too.
    expect((screen.getByTestId("secret-gate-identifier") as HTMLInputElement).value).toBe("");

    await unmount();
  });

  it("renders no identifier box when the request declares none", async () => {
    const { unmount } = await renderUI(<SecretGate approval={secretApproval()} />);

    expect(screen.queryByTestId("secret-gate-identifier")).toBeNull();

    await unmount();
  });
});
