"use client";

import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { PageHeader, StatCard, formatMoney } from "@/components/app-shell";
import { bankTransferMessage } from "@/lib/payments";
import { usePermissions } from "@/hooks/usePermissions";
import { PERMISSIONS } from "@/lib/permissions";
import {
  ArrowDownLeft,
  ArrowUpRight,
  Building2,
  CheckCircle2,
  CircleDollarSign,
  FileSpreadsheet,
  HelpCircle,
  Plus,
  Receipt,
  Smartphone,
  Upload,
  Wallet,
} from "lucide-react";

type Summary = {
  revenue: number;
  cost_of_goods: number;
  gross_profit: number;
  expenses: number;
  net_profit: number;
  purchase_payments: number;
  cash_flow: number;
};

type Expense = {
  id: string;
  category: string;
  description: string;
  amount: number;
  incurred_at: string;
};

type MpesaTx = {
  id: string;
  transaction_type: string;
  checkout_request_id: string;
  mpesa_receipt_number: string;
  phone_number: string;
  amount: number;
  status: string;
  reconciliation_status: string;
  account_reference: string;
  created_at: string;
};

type BankTx = {
  id: string;
  bank_name: string;
  reference: string;
  type: "inflow" | "outflow";
  amount: number;
  description: string;
  transacted_at: string;
  reconciliation_status: string;
};

type ReviewItem = {
  id: string;
  source: "mpesa" | "bank";
  reference: string;
  counterparty: string;
  amount: number;
  type: string;
  description: string;
  transacted_at: string;
  status: string;
};

type TabType = "overview" | "mpesa" | "bank" | "review";

/** Convert a decimal-string amount to integer cents without floating-point rounding. */
function parseCents(input: string): number {
  const t = input.trim();
  if (t === "") return 0;
  const negative = t.startsWith("-");
  const abs = negative ? t.slice(1) : t;
  const [intPart, fracPart = ""] = abs.split(".");
  const intCents = parseInt(intPart, 10) * 100;
  let fracCents = 0;
  if (fracPart.length > 0) {
    fracCents = parseInt((fracPart + "00").slice(0, 2), 10);
  }
  return negative ? -(intCents + fracCents) : intCents + fracCents;
}

export default function FinancePage() {
  const { can } = usePermissions();
  const canView = can(PERMISSIONS.financeRead);
  const canManage = can(PERMISSIONS.financeManage);

  const [activeTab, setActiveTab] = useState<TabType>("overview");
  const [d, setD] = useState<Summary | null>(null);
  const [expenses, setExpenses] = useState<Expense[]>([]);
  const [mpesaTxs, setMpesaTxs] = useState<MpesaTx[]>([]);
  const [bankTxs, setBankTxs] = useState<BankTx[]>([]);
  const [reviewItems, setReviewItems] = useState<ReviewItem[]>([]);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  // Each tab change increments the counter so a slow response from the
  // previous tab cannot overwrite state after the user has switched away.
  const [loadVersion, setLoadVersion] = useState(0);

  // Modals
  const [showExpenseForm, setShowExpenseForm] = useState(false);
  const [category, setCategory] = useState("Operations");
  const [description, setDescription] = useState("");
  const [amount, setAmount] = useState("");

  // M-Pesa STK modal
  const [showSTKModal, setShowSTKModal] = useState(false);
  const [stkPhone, setStkPhone] = useState("");
  const [stkAmount, setStkAmount] = useState("");
  const [stkRef, setStkRef] = useState("");

  // Bank manual modal
  const [showBankManualModal, setShowBankManualModal] = useState(false);
  const [bankName, setBankName] = useState("");
  const [bankRef, setBankRef] = useState("");
  const [bankType, setBankType] = useState<"inflow" | "outflow">("inflow");
  const [bankAmount, setBankAmount] = useState("");
  const [bankDesc, setBankDesc] = useState("");

  // Bank statement upload modal
  const [showStatementModal, setShowStatementModal] = useState(false);
  const [statementBank, setStatementBank] = useState("");
  const [statementFile, setStatementFile] = useState<File | null>(null);

  // Review reconcile modal
  const [reconcileTarget, setReconcileTarget] = useState<ReviewItem | null>(null);
  const [recType, setRecType] = useState<"invoice" | "customer_credit" | "expense" | "supplier_payment" | "dismiss">("invoice");
  const [recTargetID, setRecTargetID] = useState("");
  const [recExpenseCategory, setRecExpenseCategory] = useState("Operations");
  const [recNote, setRecNote] = useState("");

  /** Loads summary and tab data. */
  const loadData = useCallback(async () => {
    const myVersion = loadVersion + 1;
    setLoadVersion(myVersion);
    setError("");
    try {
      if (activeTab === "overview") {
        const [s, e] = await Promise.all([api.get("/finance/summary"), api.get("/finance/expenses")]);
        if (myVersion !== loadVersion) return;
        setD(s.data.data);
        setExpenses(e.data.data || []);
      } else if (activeTab === "mpesa") {
        const res = await api.get("/payments/mpesa/transactions");
        if (myVersion !== loadVersion) return;
        setMpesaTxs(res.data.data || []);
      } else if (activeTab === "bank") {
        const res = await api.get("/payments/bank/transactions");
        if (myVersion !== loadVersion) return;
        setBankTxs(res.data.data || []);
      } else if (activeTab === "review") {
        const res = await api.get("/payments/review-queue");
        if (myVersion !== loadVersion) return;
        setReviewItems(res.data.data || []);
      }
    } catch (e: any) {
      if (myVersion !== loadVersion) return;
      setError(e.response?.data?.error || "Failed to load data");
    }
  }, [activeTab, loadVersion]);

  useEffect(() => {
    if (canView) void loadData();
  }, [canView, loadData]);

  // Clear stale messages when the tab changes so the previous tab's
  // success/error banner does not linger.
  useEffect(() => {
    setMessage("");
  }, [activeTab]);

  /** Record expense */
  async function submitExpense(e: React.FormEvent) {
    e.preventDefault();
    const cents = parseCents(amount);
    if (!cents) {
      setError("Enter a valid amount");
      return;
    }
    try {
      await api.post("/finance/expenses", { category, description, amount: cents });
      setAmount("");
      setDescription("");
      setShowExpenseForm(false);
      setMessage(`Expense recorded successfully for ${cents / 100}.`);
      await loadData();
    } catch (e: any) {
      setError(e.response?.data?.error || "Failed to save expense");
    }
  }

  /** Send M-Pesa STK push */
  async function submitSTK(e: React.FormEvent) {
    e.preventDefault();
    const cents = parseCents(stkAmount);
    if (!cents) {
      setError("Enter a valid amount");
      return;
    }
    try {
      await api.post("/payments/mpesa/stk-push", {
        phone_number: stkPhone,
        amount: cents,
        account_reference: stkRef || "BusinessOS",
        transaction_desc: "Direct M-Pesa Payment",
        idempotency_key: `stk-${stkPhone}-${cents}-${Date.now()}`,
      });
      setShowSTKModal(false);
      setStkPhone("");
      setStkAmount("");
      setStkRef("");
      setMessage(`STK push for ${formatMoney(cents)} initiated! The customer will be prompted on their phone.`);
      await loadData();
    } catch (e: any) {
      setError(e.response?.data?.error || "Failed to initiate STK push");
    }
  }

  /** Record manual bank transfer */
  async function submitBankManual(e: React.FormEvent) {
    e.preventDefault();
    const cents = parseCents(bankAmount);
    if (!cents) {
      setError("Enter a valid amount");
      return;
    }
    try {
      const res = await api.post("/payments/bank/manual", {
        bank_name: bankName,
        reference: bankRef,
        type: bankType,
        amount: cents,
        description: bankDesc,
      });
      setShowBankManualModal(false);
      setBankRef("");
      setBankAmount("");
      setBankDesc("");
      const tx = res.data.data as BankTx;
      setMessage(bankTransferMessage(tx.amount, tx.reconciliation_status));
      await loadData();
    } catch (e: any) {
      setError(e.response?.data?.error || "Failed to record bank transfer");
    }
  }

  /** Upload bank statement CSV */
  async function submitStatement(e: React.FormEvent) {
    e.preventDefault();
    if (!statementFile) return;
    const formData = new FormData();
    formData.append("statement", statementFile);
    formData.append("bank_name", statementBank);

    try {
      const res = await api.post("/payments/bank/statement", formData, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      setShowStatementModal(false);
      setStatementFile(null);
      const r = res.data.data;
      setMessage(
        `Statement imported: ${r.imported} added (${r.reconciled} auto-reconciled, ${r.unmatched} unmatched), ${r.skipped_duplicates} duplicate(s) skipped.`,
      );
      await loadData();
    } catch (e: any) {
      setError(e.response?.data?.error || "Failed to import bank statement");
    }
  }

  /** Reconcile Review Item */
  async function submitReconcile(e: React.FormEvent) {
    e.preventDefault();
    if (!reconcileTarget) return;
    if (
      reconcileTarget.type === "outflow" &&
      (recType === "invoice" || recType === "customer_credit" || recType === "supplier_payment")
    ) {
      setError("An outflow cannot be reconciled against an invoice, customer balance, or supplier payment.");
      return;
    }
    if (
      reconcileTarget.type !== "outflow" &&
      (recType === "expense" || recType === "supplier_payment")
    ) {
      setError(recType === "supplier_payment"
        ? "An inflow cannot be reconciled as a supplier payment."
        : "An inflow cannot be booked as an operating expense.");
      return;
    }

    try {
      await api.post(`/payments/review-queue/${reconcileTarget.id}/reconcile`, {
        source: reconcileTarget.source,
        target_type: recType,
        target_id: recTargetID,
        expense_category: recExpenseCategory,
        note: recNote,
      });
      setReconcileTarget(null);
      const label = recType === "invoice" ? "invoice settlement" :
                    recType === "customer_credit" ? "customer credit" :
                    recType === "expense" ? "operating expense" :
                    recType === "supplier_payment" ? "supplier payment" :
                    "manual resolution";
      setMessage(`Transaction successfully reconciled as ${label}.`);
      await loadData();
    } catch (e: any) {
      setError(e.response?.data?.error || "Failed to reconcile transaction");
    }
  }

  if (!canView) {
    return <div className="panel p-6 text-sm text-muted">Your role does not have access to finance.</div>;
  }

  const x = d || {
    revenue: 0,
    cost_of_goods: 0,
    gross_profit: 0,
    expenses: 0,
    net_profit: 0,
    purchase_payments: 0,
    cash_flow: 0,
  };

  return (
    <>
      <PageHeader
        eyebrow="Money & Reconciliation"
        title="Finance & Payments"
        description="M-Pesa STK, C2B/B2C, bank statement reconciliation and operating expenses."
        action={
          canManage ? (
            <div className="flex flex-wrap gap-2">
              {activeTab === "overview" && (
                <button className="btn-primary" onClick={() => setShowExpenseForm(true)}>
                  <Plus size={16} /> Record expense
                </button>
              )}
              {activeTab === "mpesa" && (
                <button className="btn-primary" onClick={() => setShowSTKModal(true)}>
                  <Smartphone size={16} /> Initiate STK Push
                </button>
              )}
              {activeTab === "bank" && (
                <>
                  <button className="btn-secondary" onClick={() => setShowStatementModal(true)}>
                    <Upload size={16} /> Import Statement
                  </button>
                  <button className="btn-primary" onClick={() => setShowBankManualModal(true)}>
                    <Building2 size={16} /> Record Bank Transfer
                  </button>
                </>
              )}
            </div>
          ) : undefined
        }
      />

      {error && <p className="mb-4 rounded-lg bg-danger-soft p-3 text-sm text-danger">{error}</p>}
      {message && <p className="mb-4 rounded-lg bg-primary-soft p-3 text-sm text-primary">{message}</p>}

      {/* Tabs */}
      <div className="mb-6 flex border-b border-border-subtle">
        <button
          onClick={() => setActiveTab("overview")}
          className={`border-b-2 px-4 py-2.5 text-sm font-semibold transition-colors ${
            activeTab === "overview"
              ? "border-primary text-foreground"
              : "border-transparent text-muted hover:text-foreground"
          }`}
        >
          Overview & Expenses
        </button>
        <button
          onClick={() => setActiveTab("mpesa")}
          className={`border-b-2 px-4 py-2.5 text-sm font-semibold transition-colors ${
            activeTab === "mpesa"
              ? "border-primary text-foreground"
              : "border-transparent text-muted hover:text-foreground"
          }`}
        >
          M-Pesa Payments
        </button>
        <button
          onClick={() => setActiveTab("bank")}
          className={`border-b-2 px-4 py-2.5 text-sm font-semibold transition-colors ${
            activeTab === "bank"
              ? "border-primary text-foreground"
              : "border-transparent text-muted hover:text-foreground"
          }`}
        >
          Bank Transfers
        </button>
        <button
          onClick={() => setActiveTab("review")}
          className={`border-b-2 px-4 py-2.5 text-sm font-semibold transition-colors ${
            activeTab === "review"
              ? "border-primary text-foreground"
              : "border-transparent text-muted hover:text-foreground"
          }`}
        >
          Review Queue
        </button>
      </div>

      {/* OVERVIEW & EXPENSES TAB */}
      {activeTab === "overview" && (
        <>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <StatCard label="Revenue" value={formatMoney(x.revenue)} icon={ArrowUpRight} />
            <StatCard
              label="Net profit"
              value={formatMoney(x.net_profit)}
              note={`Gross ${formatMoney(x.gross_profit)}`}
              icon={CircleDollarSign}
            />
            <StatCard label="Operating expenses" value={formatMoney(x.expenses)} icon={Receipt} tone="amber" />
            <StatCard
              label="Cash flow"
              value={formatMoney(x.cash_flow)}
              note={`${formatMoney(x.purchase_payments)} paid to suppliers`}
              icon={Wallet}
              tone={x.cash_flow < 0 ? "amber" : "green"}
            />
          </div>

          {canManage && showExpenseForm && (
            <form onSubmit={submitExpense} className="panel mt-6 grid gap-3 p-5 sm:grid-cols-3">
              <select className="field" value={category} onChange={(e) => setCategory(e.target.value)}>
                <option>Operations</option>
                <option>Rent</option>
                <option>Utilities</option>
                <option>Transport</option>
                <option>Payroll</option>
                <option>Other</option>
              </select>
              <input
                className="field"
                placeholder="Description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
              <input
                className="field"
                type="number"
                min="0.01"
                step="0.01"
                placeholder="Amount (KSh)"
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                required
              />
              <div className="flex gap-2 sm:col-span-3">
                <button className="btn-primary">Save expense</button>
                <button type="button" className="btn-secondary" onClick={() => setShowExpenseForm(false)}>
                  Cancel
                </button>
              </div>
            </form>
          )}

          <section className="panel mt-6 overflow-hidden">
            <div className="border-b border-border-subtle p-5">
              <p className="eyebrow">Expense ledger</p>
              <h2 className="mt-1 text-lg font-bold">Recent expenses</h2>
            </div>
            {expenses.length === 0 ? (
              <p className="p-8 text-sm text-muted">No expenses recorded in this period.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <thead className="bg-surface-muted text-xs uppercase text-muted">
                    <tr>
                      <th className="px-5 py-3">Date</th>
                      <th className="px-5 py-3">Category</th>
                      <th className="px-5 py-3">Description</th>
                      <th className="px-5 py-3 text-right">Amount</th>
                    </tr>
                  </thead>
                  <tbody>
                    {expenses.map((e) => (
                      <tr key={e.id} className="border-t border-border-subtle">
                        <td className="px-5 py-4 text-muted">
                          {new Date(e.incurred_at).toLocaleDateString("en-KE")}
                        </td>
                        <td className="px-5 py-4 font-semibold">{e.category}</td>
                        <td className="px-5 py-4 text-muted">{e.description || "—"}</td>
                        <td className="px-5 py-4 text-right font-semibold text-warning-strong">
                          -{formatMoney(e.amount)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </>
      )}

      {/* M-PESA PAYMENTS TAB */}
      {activeTab === "mpesa" && (
        <section className="panel overflow-hidden">
          <div className="border-b border-border-subtle p-5">
            <p className="eyebrow">Mobile Money</p>
            <h2 className="mt-1 text-lg font-bold">M-Pesa Transactions</h2>
          </div>
          {mpesaTxs.length === 0 ? (
            <p className="p-8 text-sm text-muted">No M-Pesa transactions found.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="bg-surface-muted text-xs uppercase text-muted">
                  <tr>
                    <th className="px-5 py-3">Date</th>
                    <th className="px-5 py-3">Type</th>
                    <th className="px-5 py-3">Receipt / Ref</th>
                    <th className="px-5 py-3">Phone</th>
                    <th className="px-5 py-3 text-right">Amount</th>
                    <th className="px-5 py-3">Gateway Status</th>
                    <th className="px-5 py-3">Reconciliation</th>
                  </tr>
                </thead>
                <tbody>
                  {mpesaTxs.map((m) => (
                    <tr key={m.id} className="border-t border-border-subtle">
                      <td className="px-5 py-4 text-muted">
                        {new Date(m.created_at).toLocaleString("en-KE")}
                      </td>
                      <td className="px-5 py-4 font-semibold uppercase">{m.transaction_type}</td>
                      <td className="px-5 py-4 font-mono text-xs">
                        {m.mpesa_receipt_number || m.checkout_request_id || "—"}
                      </td>
                      <td className="px-5 py-4 text-muted">{m.phone_number}</td>
                      <td className="px-5 py-4 text-right font-semibold">
                        {formatMoney(m.amount)}
                      </td>
                      <td className="px-5 py-4">
                        <span
                          className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${
                            m.status === "completed"
                              ? "bg-success-soft text-success"
                              : m.status === "pending"
                              ? "bg-warning-soft text-warning-strong"
                              : "bg-danger-soft text-danger"
                          }`}
                        >
                          {m.status}
                        </span>
                      </td>
                      <td className="px-5 py-4">
                        <span
                          className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${
                            m.reconciliation_status === "reconciled"
                              ? "bg-primary-soft text-primary"
                              : m.reconciliation_status === "unmatched"
                              ? "bg-warning-soft text-warning-strong"
                              : "bg-surface-muted text-muted"
                          }`}
                        >
                          {m.reconciliation_status}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}

      {/* BANK TRANSFERS TAB */}
      {activeTab === "bank" && (
        <section className="panel overflow-hidden">
          <div className="border-b border-border-subtle p-5">
            <p className="eyebrow">Bank Accounts</p>
            <h2 className="mt-1 text-lg font-bold">Bank Statement & Transfers</h2>
          </div>
          {bankTxs.length === 0 ? (
            <p className="p-8 text-sm text-muted">No bank transactions recorded or imported.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="bg-surface-muted text-xs uppercase text-muted">
                  <tr>
                    <th className="px-5 py-3">Date</th>
                    <th className="px-5 py-3">Bank</th>
                    <th className="px-5 py-3">Reference</th>
                    <th className="px-5 py-3">Type</th>
                    <th className="px-5 py-3">Description</th>
                    <th className="px-5 py-3 text-right">Amount</th>
                    <th className="px-5 py-3">Reconciliation</th>
                  </tr>
                </thead>
                <tbody>
                  {bankTxs.map((b) => (
                    <tr key={b.id} className="border-t border-border-subtle">
                      <td className="px-5 py-4 text-muted">
                        {new Date(b.transacted_at).toLocaleDateString("en-KE")}
                      </td>
                      <td className="px-5 py-4 font-semibold">{b.bank_name || "—"}</td>
                      <td className="px-5 py-4 font-mono text-xs">{b.reference}</td>
                      <td className="px-5 py-4">
                        <span
                          className={`inline-flex items-center gap-1 text-xs font-semibold ${
                            b.type === "inflow" ? "text-success" : "text-warning-strong"
                          }`}
                        >
                          {b.type === "inflow" ? <ArrowDownLeft size={14} /> : <ArrowUpRight size={14} />}
                          {b.type}
                        </span>
                      </td>
                      <td className="px-5 py-4 text-muted">{b.description || "—"}</td>
                      <td className="px-5 py-4 text-right font-semibold">
                        {b.type === "outflow" ? `-${formatMoney(b.amount)}` : formatMoney(b.amount)}
                      </td>
                      <td className="px-5 py-4">
                        <span
                          className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${
                            b.reconciliation_status === "reconciled"
                              ? "bg-primary-soft text-primary"
                              : b.reconciliation_status === "unmatched"
                              ? "bg-warning-soft text-warning-strong"
                              : "bg-surface-muted text-muted"
                          }`}
                        >
                          {b.reconciliation_status}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}

      {/* REVIEW QUEUE TAB */}
      {activeTab === "review" && (
        <section className="panel overflow-hidden">
          <div className="border-b border-border-subtle p-5">
            <p className="eyebrow">Matching Queue</p>
            <h2 className="mt-1 text-lg font-bold">Unmatched Transactions</h2>
            <p className="mt-1 text-sm text-muted">
              Transactions received via M-Pesa or Bank that could not be automatically linked to an invoice or customer.
            </p>
          </div>
          {reviewItems.length === 0 ? (
            <p className="p-8 text-sm text-muted">Review queue is empty! All transactions are reconciled.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="bg-surface-muted text-xs uppercase text-muted">
                  <tr>
                    <th className="px-5 py-3">Source</th>
                    <th className="px-5 py-3">Reference</th>
                    <th className="px-5 py-3">Party / Bank</th>
                    <th className="px-5 py-3">Description</th>
                    <th className="px-5 py-3 text-right">Amount</th>
                    <th className="px-5 py-3 text-right">Action</th>
                  </tr>
                </thead>
                <tbody>
                  {reviewItems.map((item) => (
                    <tr key={item.id} className="border-t border-border-subtle">
                      <td className="px-5 py-4 font-semibold uppercase">{item.source}</td>
                      <td className="px-5 py-4 font-mono text-xs">{item.reference}</td>
                      <td className="px-5 py-4">{item.counterparty || "—"}</td>
                      <td className="px-5 py-4 text-muted">{item.description || "—"}</td>
                      <td className="px-5 py-4 text-right font-semibold">{formatMoney(item.amount)}</td>
                      <td className="px-5 py-4 text-right">
                        {canManage && (
                          <button
                            className="btn-secondary py-1 text-xs"
                            onClick={() => {
                              setReconcileTarget(item);
                              setRecType(item.type === "outflow" ? "expense" : "invoice");
                              setRecTargetID("");
                              setRecNote(`Reconciled ${item.source} ref: ${item.reference}`);
                            }}
                          >
                            Reconcile
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}

      {/* MODAL: M-Pesa STK Push */}
      {showSTKModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <form onSubmit={submitSTK} className="panel w-full max-w-md p-6">
            <h3 className="text-lg font-bold">Initiate M-Pesa STK Push</h3>
            <p className="mt-1 text-xs text-muted">
              Sends an instant payment prompt to the customer&apos;s phone.
            </p>
            <div className="mt-4 space-y-3">
              <div>
                <label className="block text-xs font-semibold text-muted">Customer Phone</label>
                <input
                  className="field mt-1"
                  placeholder="0712345678 or 254712345678"
                  value={stkPhone}
                  onChange={(e) => setStkPhone(e.target.value)}
                  required
                />
              </div>
              <div>
                <label className="block text-xs font-semibold text-muted">Amount (KSh)</label>
                <input
                  className="field mt-1"
                  type="number"
                  min="1"
                  step="0.01"
                  placeholder="100.00"
                  value={stkAmount}
                  onChange={(e) => setStkAmount(e.target.value)}
                  required
                />
              </div>
              <div>
                <label className="block text-xs font-semibold text-muted">Account Reference / Invoice #</label>
                <input
                  className="field mt-1"
                  placeholder="e.g. INV-2026-0001"
                  value={stkRef}
                  onChange={(e) => setStkRef(e.target.value)}
                />
              </div>
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button type="button" className="btn-secondary" onClick={() => setShowSTKModal(false)}>
                Cancel
              </button>
              <button className="btn-primary">Send Prompt</button>
            </div>
          </form>
        </div>
      )}

      {/* MODAL: Manual Bank Transfer */}
      {showBankManualModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <form onSubmit={submitBankManual} className="panel w-full max-w-md p-6">
            <h3 className="text-lg font-bold">Record Bank Transfer</h3>
            <div className="mt-4 space-y-3">
              <div>
                <label className="block text-xs font-semibold text-muted">Bank Name</label>
                <input
                  className="field mt-1"
                  placeholder="e.g. Equity Bank, KCB, Standard Chartered"
                  value={bankName}
                  onChange={(e) => setBankName(e.target.value)}
                />
              </div>
              <div>
                <label className="block text-xs font-semibold text-muted">Reference Number</label>
                <input
                  className="field mt-1"
                  placeholder="Unique bank transfer reference"
                  value={bankRef}
                  onChange={(e) => setBankRef(e.target.value)}
                  required
                />
              </div>
              <div>
                <label className="block text-xs font-semibold text-muted">Type</label>
                <select
                  className="field mt-1"
                  value={bankType}
                  onChange={(e) => setBankType(e.target.value as "inflow" | "outflow")}
                >
                  <option value="inflow">Inflow (Money Received / Invoice Settlement)</option>
                  <option value="outflow">Outflow (Expense / Supplier Payment)</option>
                </select>
              </div>
              <div>
                <label className="block text-xs font-semibold text-muted">Amount (KSh)</label>
                <input
                  className="field mt-1"
                  type="number"
                  min="0.01"
                  step="0.01"
                  placeholder="100.00"
                  value={bankAmount}
                  onChange={(e) => setBankAmount(e.target.value)}
                  required
                />
              </div>
              <div>
                <label className="block text-xs font-semibold text-muted">Description</label>
                <input
                  className="field mt-1"
                  placeholder="e.g. Payment for INV-2026-0042"
                  value={bankDesc}
                  onChange={(e) => setBankDesc(e.target.value)}
                />
              </div>
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button type="button" className="btn-secondary" onClick={() => setShowBankManualModal(false)}>
                Cancel
              </button>
              <button className="btn-primary">Save Transfer</button>
            </div>
          </form>
        </div>
      )}

      {/* MODAL: Bank Statement CSV Upload */}
      {showStatementModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <form onSubmit={submitStatement} className="panel w-full max-w-md p-6">
            <h3 className="text-lg font-bold">Import Bank Statement (CSV)</h3>
            <p className="mt-1 text-xs text-muted">
              Upload standard bank statement CSV with Date, Reference, Description, and Amount columns.
            </p>
            <div className="mt-4 space-y-3">
              <div>
                <label className="block text-xs font-semibold text-muted">Bank Name</label>
                <input
                  className="field mt-1"
                  placeholder="e.g. Absa Bank"
                  value={statementBank}
                  onChange={(e) => setStatementBank(e.target.value)}
                />
              </div>
              <div>
                <label className="block text-xs font-semibold text-muted">Select CSV File</label>
                <input
                  type="file"
                  accept=".csv"
                  className="field mt-1"
                  onChange={(e) => setStatementFile(e.target.files?.[0] || null)}
                  required
                />
              </div>
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button type="button" className="btn-secondary" onClick={() => setShowStatementModal(false)}>
                Cancel
              </button>
              <button className="btn-primary" disabled={!statementFile}>
                Upload & Reconcile
              </button>
            </div>
          </form>
        </div>
      )}

      {/* MODAL: Reconcile Review Item */}
      {reconcileTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <form onSubmit={submitReconcile} className="panel w-full max-w-md p-6">
            <h3 className="text-lg font-bold">Reconcile Transaction</h3>
            <p className="mt-1 text-xs text-muted">
              Source: <span className="font-semibold uppercase">{reconcileTarget.source}</span> | Ref:{" "}
              <span className="font-mono">{reconcileTarget.reference}</span> | Amount:{" "}
              <span className="font-semibold text-primary">{formatMoney(reconcileTarget.amount)}</span>
            </p>
            <div className="mt-4 space-y-3">
              <div>
                <label className="block text-xs font-semibold text-muted">Reconcile As</label>
                <select
                  className="field mt-1"
                  value={recType}
                  onChange={(e) => {
                    setRecType(e.target.value as typeof recType);
                    setRecTargetID("");
                  }}
                >
                  {reconcileTarget.type !== "outflow" && (
                    <>
                      <option value="invoice">Invoice Settlement</option>
                      <option value="customer_credit">Customer Credit / Balance</option>
                    </>
                  )}
                  {reconcileTarget.type === "outflow" && (
                    <>
                      <option value="expense">Operating Expense</option>
                      <option value="supplier_payment">Supplier Payment</option>
                    </>
                  )}
                  <option value="dismiss">Dismiss / Manual Resolution</option>
                </select>
                <p className="mt-1 text-xs text-subtle">
                  {reconcileTarget.type === "outflow"
                    ? "Money left the business, so this can only be booked as an expense."
                    : "Money arrived, so this can settle an invoice or customer balance."}
                </p>
              </div>

              {recType === "invoice" && (
                <div>
                  <label className="block text-xs font-semibold text-muted">Invoice ID</label>
                  <input
                    className="field mt-1 font-mono"
                    placeholder="Invoice UUID (e.g. from invoices page)"
                    value={recTargetID}
                    onChange={(e) => setRecTargetID(e.target.value)}
                    required
                  />
                </div>
              )}

              {recType === "customer_credit" && (
                <div>
                  <label className="block text-xs font-semibold text-muted">Customer ID</label>
                  <input
                    className="field mt-1 font-mono"
                    placeholder="Customer UUID"
                    value={recTargetID}
                    onChange={(e) => setRecTargetID(e.target.value)}
                    required
                  />
                </div>
              )}

              {recType === "expense" && (
                <div>
                  <label className="block text-xs font-semibold text-muted">Expense Category</label>
                  <select
                    className="field mt-1"
                    value={recExpenseCategory}
                    onChange={(e) => setRecExpenseCategory(e.target.value)}
                  >
                    <option>Operations</option>
                    <option>Rent</option>
                    <option>Utilities</option>
                    <option>Transport</option>
                    <option>Payroll</option>
                    <option>Other</option>
                  </select>
                </div>
              )}

              {recType === "supplier_payment" && (
                <div>
                  <label className="block text-xs font-semibold text-muted">Supplier ID</label>
                  <input
                    className="field mt-1 font-mono"
                    placeholder="Supplier UUID"
                    value={recTargetID}
                    onChange={(e) => setRecTargetID(e.target.value)}
                    required
                  />
                </div>
              )}

              <div>
                <label className="block text-xs font-semibold text-muted">Note</label>
                <input
                  className="field mt-1"
                  value={recNote}
                  onChange={(e) => setRecNote(e.target.value)}
                />
              </div>
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button type="button" className="btn-secondary" onClick={() => setReconcileTarget(null)}>
                Cancel
              </button>
              <button className="btn-primary">Apply Reconciliation</button>
            </div>
          </form>
        </div>
      )}
    </>
  );
}
