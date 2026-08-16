"use client";

import { useCallback, useEffect, useState } from "react";
import {
  ArrowDownLeft,
  ArrowUpRight,
  Copy,
  Loader2,
  PiggyBank,
  RefreshCw,
  Save,
  ShieldCheck,
} from "lucide-react";
import { useAccount } from "../account-context";
import { tokenManagerConfig } from "../config";
import {
  formatDate,
  middleEllipsis,
  rawToUiAmount,
  uiAmountToRaw,
} from "../format";
import { getAuthJwt } from "../neon-auth";
import {
  createWithdrawal,
  getBillingState,
  listBillingDeposits,
  listBillingLedger,
  listWithdrawals,
  updateBillingPreferences,
  type BillingCaps,
  type BillingDepositEntry,
  type BillingLedgerEntry,
  type BillingState,
  type BillingWithdrawal,
  type BillingWithdrawalState,
} from "../manage-api";

/**
 * BillingPanel renders the on-chain credit account behind the Neon identity:
 * available/reserved balance, deposit instructions for the treasury ATA, the
 * three buyer price caps, and the recent immutable ledger of every balance
 * movement (usage, earnings, fees, deposits). The data is fetched from
 * /manage/billing, which is JWT-authenticated by the surrounding manage
 * router — the same flow the faucet uses.
 */
export default function BillingPanel() {
  const { neonUser, showNotice, pending, setPending, releasePending } =
    useAccount();

  const [state, setState] = useState<BillingState | null>(null);
  const [ledger, setLedger] = useState<BillingLedgerEntry[]>([]);
  const [deposits, setDeposits] = useState<BillingDepositEntry[]>([]);
  const [withdrawals, setWithdrawals] = useState<BillingWithdrawal[]>([]);
  // The three buyer caps. An empty input clears that dimension to "unlimited";
  // the API accepts null. We keep local string state so the form is editable
  // before the first save.
  const [inputCap, setInputCap] = useState("");
  const [cachedCap, setCachedCap] = useState("");
  const [outputCap, setOutputCap] = useState("");
  // Withdraw form. The destination comes from the account's primary linked
  // wallet; the user only enters an OTELA amount. The idempotency key is
  // generated client-side so a retried submit never double-debits.
  const [withdrawAmount, setWithdrawAmount] = useState("");

  const decimals = state?.deposits.decimals ?? tokenManagerConfig.otelaDecimals;

  const load = useCallback(async () => {
    if (!neonUser) return;
    const jwt = await getAuthJwt();
    const [next, ledgerPage, depositPage, withdrawalPage] = await Promise.all([
      getBillingState(tokenManagerConfig.apiBaseUrl, jwt),
      listBillingLedger(tokenManagerConfig.apiBaseUrl, jwt, undefined, 10),
      listBillingDeposits(tokenManagerConfig.apiBaseUrl, jwt, undefined, 10),
      listWithdrawals(tokenManagerConfig.apiBaseUrl, jwt, undefined, 10),
    ]);
    setState(next);
    setLedger(ledgerPage.entries);
    setDeposits(depositPage.entries);
    setWithdrawals(withdrawalPage.entries);
  }, [neonUser]);

  useEffect(() => {
    if (!neonUser) {
      setState(null);
      setLedger([]);
      setDeposits([]);
      setWithdrawals([]);
      return;
    }
    load().catch((error: unknown) =>
      showNotice(
        "error",
        error instanceof Error ? error.message : String(error),
      ),
    );
  }, [neonUser, load, showNotice]);

  // Seed the cap inputs once the server state arrives, and again if the
  // server's caps change out from under us (e.g. another tab edited them).
  useEffect(() => {
    if (!state) return;
    setInputCap(
      state.caps.input_per_million == null
        ? ""
        : String(state.caps.input_per_million),
    );
    setCachedCap(
      state.caps.cached_input_per_million == null
        ? ""
        : String(state.caps.cached_input_per_million),
    );
    setOutputCap(
      state.caps.output_per_million == null
        ? ""
        : String(state.caps.output_per_million),
    );
  }, [state]);

  async function copyToClipboard(value: string, label: string) {
    try {
      await navigator.clipboard.writeText(value);
      showNotice("success", `${label} copied`);
    } catch {
      showNotice(
        "error",
        `Copy failed — select the ${label.toLowerCase()} and copy manually`,
      );
    }
  }

  function parseCap(raw: string): number | null {
    const trimmed = raw.trim();
    if (trimmed === "") return null; // empty = unlimited
    const parsed = Number(trimmed);
    if (!Number.isFinite(parsed) || parsed < 0) {
      throw new Error(
        "Caps must be a non-negative number (or empty for unlimited)",
      );
    }
    return parsed;
  }

  async function handleSaveCaps(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!state) return;
    setPending("billing-caps");
    try {
      const caps: BillingCaps = {
        input_per_million: parseCap(inputCap),
        cached_input_per_million: parseCap(cachedCap),
        output_per_million: parseCap(outputCap),
      };
      const returned = await updateBillingPreferences(
        tokenManagerConfig.apiBaseUrl,
        await getAuthJwt(),
        caps,
      );
      setState((prev) => (prev ? { ...prev, caps: returned } : prev));
      showNotice("success", "Price caps updated");
    } catch (error) {
      showNotice(
        "error",
        error instanceof Error ? error.message : String(error),
      );
    } finally {
      releasePending("billing-caps");
    }
  }

  function rawToUi(raw: bigint): string {
    return rawToUiAmount(raw, decimals);
  }

  async function handleWithdraw(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!state) return;
    if (!state.withdrawals_enabled) {
      showNotice("error", "Withdrawals are not enabled on this deployment");
      return;
    }
    const primaryLinkedWallet = state.primary_linked_wallet?.trim() ?? "";
    if (!primaryLinkedWallet) {
      showNotice("error", "Link a primary wallet before withdrawing earnings");
      return;
    }
    let raw: bigint;
    try {
      raw = uiAmountToRaw(withdrawAmount, decimals);
    } catch (error) {
      showNotice(
        "error",
        error instanceof Error ? error.message : "Enter a valid OTELA amount",
      );
      return;
    }
    if (raw > state.balance.available_raw) {
      showNotice("error", "Not enough available credit for that withdrawal");
      return;
    }
    setPending("billing-withdraw");
    try {
      const created = await createWithdrawal(
        tokenManagerConfig.apiBaseUrl,
        await getAuthJwt(),
        {
          primary_linked_wallet: primaryLinkedWallet,
          amount_raw: raw,
          idempotency_key:
            typeof crypto !== "undefined" && "randomUUID" in crypto
              ? crypto.randomUUID()
              : `withdraw-${Date.now()}-${Math.random().toString(36).slice(2)}`,
        },
      );
      setWithdrawals((prev) => [created, ...prev].slice(0, 20));
      setWithdrawAmount("");
      showNotice("success", "Withdrawal queued — finalizing in a moment");
      // Refresh balance immediately (credit is debited at reserve time) and
      // poll the list a little later so the state badge advances.
      load().catch(() => {
        /* a later refresh will catch up */
      });
    } catch (error) {
      showNotice(
        "error",
        error instanceof Error ? error.message : String(error),
      );
    } finally {
      releasePending("billing-withdraw");
    }
  }

  if (!neonUser) {
    return (
      <section className="otm-panel otm-billing-panel">
        <div className="otm-panel-heading">
          <div>
            <p className="otm-eyebrow">Billing</p>
            <h2>Credit account</h2>
          </div>
        </div>
        <div className="otm-empty-state">
          Sign in with OpenTela to view your on-chain credit account.
        </div>
      </section>
    );
  }

  if (state === null) {
    return (
      <section className="otm-panel otm-billing-panel">
        <div className="otm-panel-heading">
          <div>
            <p className="otm-eyebrow">Billing</p>
            <h2>Credit account</h2>
          </div>
        </div>
        <div className="otm-empty-state">Loading credit account…</div>
      </section>
    );
  }

  return (
    <section className="otm-panel otm-billing-panel">
      <div className="otm-panel-heading">
        <div>
          <p className="otm-eyebrow">
            Billing · <span className="otm-mode-chip">{state.mode}</span>
          </p>
          <h2>Credit account</h2>
        </div>
        <button
          type="button"
          className="otm-icon-button"
          onClick={() =>
            load().catch((error: unknown) =>
              showNotice(
                "error",
                error instanceof Error ? error.message : String(error),
              ),
            )
          }
          title="Refresh billing"
        >
          {pending === "billing-refresh" ? (
            <Loader2 className="otm-spin" size={18} />
          ) : (
            <RefreshCw size={18} />
          )}
        </button>
      </div>

      <div className="otm-status-grid otm-billing-grid">
        <div className="otm-metric-panel accent">
          <span>Available</span>
          <strong title={`${state.balance.available_raw} raw`}>
            {rawToUi(state.balance.available_raw)}
          </strong>
          <small>OTELA · spendable now</small>
        </div>
        <div className="otm-metric-panel">
          <span>Reserved</span>
          <strong title={`${state.balance.reserved_raw} raw`}>
            {rawToUi(state.balance.reserved_raw)}
          </strong>
          <small>In-flight requests</small>
        </div>
        <div className="otm-metric-panel">
          <span>Total credit</span>
          <strong title={`${state.balance.credit_raw} raw`}>
            {rawToUi(state.balance.credit_raw)}
          </strong>
          <small>
            Updated{" "}
            {state.balance.updated_at
              ? formatDate(state.balance.updated_at)
              : "—"}
          </small>
        </div>
      </div>

      <DepositInstructions state={state} onCopy={copyToClipboard} />

      <form className="otm-caps-form" onSubmit={handleSaveCaps}>
        <div className="otm-caps-heading">
          <ShieldCheck size={16} />
          <div>
            <strong>Buyer price caps</strong>
            <p>
              The most you'll pay per million tokens. Leave a field empty for no
              limit on that tier; <code>0</code> means free peers only.
            </p>
          </div>
        </div>
        <div className="otm-caps-fields">
          <label>
            <span>Input /M</span>
            <input
              inputMode="decimal"
              value={inputCap}
              onChange={(e) => setInputCap(e.target.value)}
              placeholder="unlimited"
              spellCheck={false}
            />
          </label>
          <label>
            <span>Cached /M</span>
            <input
              inputMode="decimal"
              value={cachedCap}
              onChange={(e) => setCachedCap(e.target.value)}
              placeholder="unlimited"
              spellCheck={false}
            />
          </label>
          <label>
            <span>Output /M</span>
            <input
              inputMode="decimal"
              value={outputCap}
              onChange={(e) => setOutputCap(e.target.value)}
              placeholder="unlimited"
              spellCheck={false}
            />
          </label>
        </div>
        <button
          type="submit"
          className="otm-primary-button"
          disabled={pending === "billing-caps"}
        >
          {pending === "billing-caps" ? (
            <Loader2 className="otm-spin" size={16} />
          ) : (
            <Save size={16} />
          )}
          Save caps
        </button>
      </form>

      <WithdrawForm
        state={state}
        withdrawAmount={withdrawAmount}
        setWithdrawAmount={setWithdrawAmount}
        pending={pending}
        onSubmit={handleWithdraw}
      />
      <Withdrawals entries={withdrawals} decimals={decimals} />

      <Ledger entries={ledger} decimals={decimals} onCopy={copyToClipboard} />
      <RecentDeposits entries={deposits} decimals={decimals} />
    </section>
  );
}

function DepositInstructions({
  state,
  onCopy,
}: {
  state: BillingState;
  onCopy: (value: string, label: string) => void;
}) {
  if (!state.deposits.enabled) {
    return (
      <div className="otm-billing-instructions otm-empty-state">
        On-chain deposits are not enabled on this deployment.
      </div>
    );
  }
  const treasury = state.deposits.treasury_ata;
  const mint = state.deposits.mint;
  return (
    <div className="otm-billing-instructions">
      <div className="otm-caps-heading">
        <PiggyBank size={16} />
        <div>
          <strong>Deposit OTELA</strong>
          <p>
            Send SPL token OTELA to the treasury associated token account. Funds
            are credited automatically once the transfer is finalized (about a
            few seconds on devnet).
          </p>
        </div>
      </div>
      <dl className="otm-deposit-facts">
        <div>
          <dt>Treasury ATA</dt>
          <dd>
            <code title={treasury ?? ""}>
              {treasury ? middleEllipsis(treasury, 8, 8) : "—"}
            </code>
            {treasury ? (
              <button
                type="button"
                className="otm-icon-button"
                onClick={() => onCopy(treasury, "Treasury ATA")}
                title="Copy treasury ATA"
              >
                <Copy size={14} />
              </button>
            ) : null}
          </dd>
        </div>
        <div>
          <dt>Mint</dt>
          <dd>
            <code title={mint ?? ""}>
              {mint ? middleEllipsis(mint, 8, 8) : "—"}
            </code>
            {mint ? (
              <button
                type="button"
                className="otm-icon-button"
                onClick={() => onCopy(mint, "Mint")}
                title="Copy mint"
              >
                <Copy size={14} />
              </button>
            ) : null}
          </dd>
        </div>
      </dl>
    </div>
  );
}

function withdrawLabel(state: BillingWithdrawalState): string {
  switch (state) {
    case "reserved":
      return "queued";
    case "signed":
      return "signing";
    case "broadcast":
      return "broadcasting";
    case "finalized":
      return "finalized";
    case "failed":
      return "failed";
    case "restored":
      return "restored";
    default:
      return state;
  }
}

function WithdrawForm({
  state,
  withdrawAmount,
  setWithdrawAmount,
  pending,
  onSubmit,
}: {
  state: BillingState;
  withdrawAmount: string;
  setWithdrawAmount: (value: string) => void;
  pending: string | null;
  onSubmit: (event: React.FormEvent<HTMLFormElement>) => void;
}) {
  const decimals = state.deposits.decimals ?? tokenManagerConfig.otelaDecimals;
  const available = rawToUiAmount(state.balance.available_raw, decimals);
  const primaryLinkedWallet = state.primary_linked_wallet;
  const canWithdraw = state.withdrawals_enabled && Boolean(primaryLinkedWallet);
  const helperText = !state.withdrawals_enabled
    ? "Withdrawals are not enabled on this deployment."
    : primaryLinkedWallet
      ? "OpenTela sends OTELA to your primary linked wallet automatically."
      : "Link a primary wallet before withdrawing earnings.";

  if (!state.withdrawals_enabled) {
    return null;
  }

  return (
    <form className="otm-caps-form otm-withdraw-form" onSubmit={onSubmit}>
      <div className="otm-caps-heading">
        <ArrowUpRight size={16} />
        <div>
          <strong>Withdraw earnings</strong>
          <p>
            Move on-chain OTELA from your credit account to your primary linked
            wallet. The transfer is signed and broadcast by OpenTela and
            finalizes on Solana in a few seconds.
          </p>
        </div>
      </div>
      <div className="otm-caps-fields">
        <label>
          <span>Primary linked wallet</span>
          <input
            value={primaryLinkedWallet ?? "No primary linked wallet"}
            readOnly
            spellCheck={false}
            autoComplete="off"
          />
        </label>
        <label>
          <span>Amount (OTELA)</span>
          <input
            inputMode="decimal"
            value={withdrawAmount}
            onChange={(e) => setWithdrawAmount(e.target.value)}
            placeholder={`up to ${available}`}
            spellCheck={false}
            disabled={!canWithdraw}
          />
        </label>
      </div>
      <p className="otm-panel-note">{helperText}</p>
      <button
        type="submit"
        className="otm-primary-button"
        disabled={!canWithdraw || pending === "billing-withdraw"}
      >
        {pending === "billing-withdraw" ? (
          <Loader2 className="otm-spin" size={16} />
        ) : (
          <ArrowUpRight size={16} />
        )}
        Withdraw
      </button>
    </form>
  );
}

function Withdrawals({
  entries,
  decimals,
}: {
  entries: BillingWithdrawal[];
  decimals: number;
}) {
  if (entries.length === 0) return null;
  return (
    <div className="otm-billing-withdrawals">
      <h3>Withdrawals</h3>
      <ul className="otm-ledger-list">
        {entries.map((entry) => (
          <li
            key={entry.id}
            className={entry.state === "finalized" ? "otm-credit" : "otm-debit"}
          >
            <ArrowUpRight size={14} />
            <span className="otm-ledger-amount">
              {rawToUiAmount(entry.amount_raw, decimals)} OTELA
            </span>
            <span
              className={`otm-withdraw-status otm-withdraw-status-${entry.state}`}
              title={entry.error ?? undefined}
            >
              {withdrawLabel(entry.state)}
            </span>
            <span className="otm-ledger-model" title={entry.destination_wallet}>
              to {middleEllipsis(entry.destination_wallet, 6, 6)}
            </span>
            <span className="otm-ledger-time">
              {entry.finalized_at
                ? formatDate(entry.finalized_at)
                : entry.broadcast_at
                  ? formatDate(entry.broadcast_at)
                  : entry.signed_at
                    ? formatDate(entry.signed_at)
                    : formatDate(entry.reserved_at)}
            </span>
            {entry.signature ? (
              <code title={entry.signature}>
                {middleEllipsis(entry.signature, 6, 6)}
              </code>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  );
}

function describeEntry(entry: BillingLedgerEntry, decimals: number): string {
  const ui = rawToUiAmount(
    entry.delta_raw < 0n ? -entry.delta_raw : entry.delta_raw,
    decimals,
  );
  const verb = entry.delta_raw < 0n ? "−" : "+";
  if (entry.source === "usage") return `${verb}${ui} usage`;
  if (entry.source === "earn") return `${verb}${ui} earnings`;
  if (entry.source === "fee") return `${verb}${ui} routing fee`;
  if (entry.source === "deposit") return `${verb}${ui} deposit`;
  if (entry.source === "withdraw") return `${verb}${ui} withdrawal`;
  return `${verb}${ui} ${entry.source}`;
}

function Ledger({
  entries,
  decimals,
  onCopy,
}: {
  entries: BillingLedgerEntry[];
  decimals: number;
  onCopy: (value: string, label: string) => void;
}) {
  if (entries.length === 0) {
    return (
      <div className="otm-billing-ledger">
        <h3>Recent activity</h3>
        <div className="otm-empty-state">
          No usage or earnings yet. Once you route inference through OpenTela,
          every balance movement is recorded here.
        </div>
      </div>
    );
  }
  return (
    <div className="otm-billing-ledger">
      <h3>Recent activity</h3>
      <ul className="otm-ledger-list">
        {entries.map((entry) => (
          <li
            key={entry.id}
            className={entry.delta_raw < 0n ? "otm-debit" : "otm-credit"}
          >
            <span className="otm-ledger-amount">
              {describeEntry(entry, decimals)}
            </span>
            {entry.model ? (
              <span className="otm-ledger-model">{entry.model}</span>
            ) : null}
            <span className="otm-ledger-time">
              {entry.created_at ? formatDate(entry.created_at) : ""}
            </span>
            {entry.ref ? (
              <button
                type="button"
                className="otm-text-button"
                onClick={() => onCopy(entry.ref, "Reference")}
                title="Copy reference"
              >
                {middleEllipsis(entry.ref, 6, 4)}
              </button>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  );
}

function RecentDeposits({
  entries,
  decimals,
}: {
  entries: BillingDepositEntry[];
  decimals: number;
}) {
  if (entries.length === 0) return null;
  return (
    <div className="otm-billing-deposits">
      <h3>Recent deposits</h3>
      <ul className="otm-ledger-list">
        {entries.map((entry) => (
          <li
            key={`${entry.transaction_signature}:${entry.instruction_index}`}
            className="otm-credit"
          >
            <ArrowDownLeft size={14} />
            <span className="otm-ledger-amount">
              +{rawToUiAmount(entry.amount_raw, decimals)} OTELA
            </span>
            <span className="otm-ledger-model" title={entry.from_wallet}>
              from {middleEllipsis(entry.from_wallet, 6, 6)}
            </span>
            <span className="otm-ledger-time">
              {entry.credited_at
                ? formatDate(entry.credited_at)
                : entry.assignment_state === "unassigned"
                  ? "awaiting wallet link"
                  : formatDate(entry.seen_at)}
            </span>
            <code title={entry.transaction_signature}>
              {middleEllipsis(entry.transaction_signature, 6, 6)}
            </code>
          </li>
        ))}
      </ul>
    </div>
  );
}
