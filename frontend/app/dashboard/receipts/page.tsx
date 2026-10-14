"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { PageHeader, formatMoney } from "@/components/app-shell";
import { AlertTriangle, Check, Plus, ScanLine, Trash2, Upload } from "lucide-react";

interface Product {
  id: string;
  name: string;
  unit: string;
  price: number; // cents
}

interface Supplier {
  id: string;
  name: string;
}

interface Customer {
  id: string;
  name: string;
}

interface ScannedLine {
  description: string;
  quantity: number;
  unit_price: number; // cents
  subtotal: number; // cents
  matched_product_id?: string;
  matched_product_name?: string;
}

interface ScanResult {
  merchant: string;
  date: string;
  currency: string;
  line_items: ScannedLine[];
  total: number; // cents
  confidence: string;
  notes: string;
}

interface ReviewLine {
  description: string;
  productId: string;
  quantity: string;
  unitPrice: string; // major units, as typed
}

type RecordType = "purchase" | "sale";
type SaleType = "cash" | "credit" | "quotation";

function centsToInput(cents: number): string {
  return (Number(cents || 0) / 100).toFixed(2);
}

function inputToCents(value: string): number {
  return Math.round(Number(value || 0) * 100);
}

export default function ReceiptsPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [suppliers, setSuppliers] = useState<Supplier[]>([]);
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [loadError, setLoadError] = useState("");

  const [file, setFile] = useState<File | null>(null);
  const [previewUrl, setPreviewUrl] = useState("");
  const [scanning, setScanning] = useState(false);
  const [scanError, setScanError] = useState("");
  const [scan, setScan] = useState<ScanResult | null>(null);

  const [lines, setLines] = useState<ReviewLine[]>([]);
  const [recordType, setRecordType] = useState<RecordType>("purchase");
  const [supplierId, setSupplierId] = useState("");
  const [customerId, setCustomerId] = useState("");
  const [saleType, setSaleType] = useState<SaleType>("cash");
  const [note, setNote] = useState("");
  const [amountPaid, setAmountPaid] = useState("");

  const [committing, setCommitting] = useState(false);
  const [commitError, setCommitError] = useState("");
  const [saved, setSaved] = useState<{ record_type: string; id: string; total_amount: number } | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  // Bumped whenever the selected file changes so a slow scan of a previously
  // selected image can be ignored instead of repopulating the review form.
  const scanRequestRef = useRef(0);

  useEffect(() => {
    async function load() {
      try {
        const [productsRes, suppliersRes, customersRes] = await Promise.all([
          api.get("/products"),
          api.get("/suppliers"),
          api.get("/customers"),
        ]);
        setProducts(productsRes.data.data || []);
        setSuppliers(suppliersRes.data.data || []);
        setCustomers(customersRes.data.data || []);
      } catch (err: any) {
        setLoadError(err.response?.data?.error || "Failed to load products, suppliers and customers");
      }
    }
    load();
  }, []);

  useEffect(() => {
    if (!file) {
      setPreviewUrl("");
      return;
    }
    const url = URL.createObjectURL(file);
    setPreviewUrl(url);
    return () => URL.revokeObjectURL(url);
  }, [file]);

  function pickFile(next: File | null) {
    scanRequestRef.current += 1;
    setFile(next);
    setScan(null);
    setLines([]);
    setScanError("");
    setCommitError("");
    setSaved(null);
  }

  async function handleScan() {
    if (!file) return;
    const requestId = scanRequestRef.current;
    setScanning(true);
    setScanError("");
    setCommitError("");
    setSaved(null);
    try {
      const form = new FormData();
      form.append("image", file);
      const res = await api.post("/receipts/scan", form, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      if (requestId !== scanRequestRef.current) return;
      const result: ScanResult = res.data.data;
      setScan(result);
      setLines(
        (result.line_items || []).map((line) => ({
          description: line.description,
          productId: line.matched_product_id || "",
          quantity: String(line.quantity || 1),
          unitPrice: centsToInput(line.unit_price),
        })),
      );
    } catch (err: any) {
      if (requestId !== scanRequestRef.current) return;
      setScanError(err.response?.data?.error || "Could not read that receipt. Try a clearer photo.");
    } finally {
      if (requestId === scanRequestRef.current) setScanning(false);
    }
  }

  function updateLine(index: number, key: keyof ReviewLine, value: string) {
    setLines((prev) => prev.map((line, i) => (i === index ? { ...line, [key]: value } : line)));
  }

  function addLine() {
    setLines((prev) => [...prev, { description: "", productId: "", quantity: "1", unitPrice: "0.00" }]);
  }

  function removeLine(index: number) {
    setLines((prev) => prev.filter((_, i) => i !== index));
  }

  const estimatedTotal = useMemo(
    () => lines.reduce((sum, line) => sum + inputToCents(line.unitPrice) * (Number(line.quantity) || 0), 0),
    [lines],
  );

  const reviewReady =
    lines.length > 0 &&
    lines.every((line) => line.productId !== "" && Number(line.quantity) > 0) &&
    (recordType === "sale" || supplierId !== "");

  async function handleCommit() {
    setCommitting(true);
    setCommitError("");
    try {
      const payload: any = {
        record_type: recordType,
        note,
        line_items: lines.map((line) => ({
          product_id: line.productId,
          quantity: Number(line.quantity) || 0,
          unit_price: inputToCents(line.unitPrice),
        })),
      };
      if (recordType === "purchase") {
        payload.supplier_id = supplierId;
        payload.amount_paid = inputToCents(amountPaid);
      } else {
        payload.sale_type = saleType;
        if (customerId) payload.customer_id = customerId;
      }

      const res = await api.post("/receipts/commit", payload);
      setSaved(res.data.data);
      setScan(null);
      setLines([]);
      setFile(null);
      if (fileInput.current) fileInput.current.value = "";
    } catch (err: any) {
      setCommitError(err.response?.data?.error || "Failed to save the record");
    } finally {
      setCommitting(false);
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Operations"
        title="Receipt OCR"
        description="Photograph a receipt, review what was read, then save it as a purchase or a sale."
      />

      {loadError && <p className="mb-4 rounded-lg bg-red-50 p-3 text-sm text-red-700">{loadError}</p>}

      {saved && (
        <div className="mb-6 flex items-center gap-2 rounded-lg bg-[#e7f4ec] p-4 text-sm text-[#12633e]">
          <Check size={18} />
          <p>
            Saved as a {saved.record_type} — {formatMoney(saved.total_amount)}. Reference {saved.id.slice(0, 8).toUpperCase()}.
          </p>
        </div>
      )}

      <div className="grid gap-6 xl:grid-cols-[.8fr_1.2fr]">
        <section className="panel h-fit space-y-4 p-5">
          <p className="eyebrow">1. Upload a receipt</p>
          <label className="flex cursor-pointer flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-[#cfd8d0] p-6 text-center hover:bg-[#f7f9f6]">
            <Upload size={22} className="text-[#16794c]" />
            <span className="text-sm font-medium">{file ? file.name : "Choose or take a photo"}</span>
            <span className="text-xs text-[#8a958d]">JPEG, PNG or WebP · up to 10 MB</span>
            <input
              ref={fileInput}
              type="file"
              accept="image/jpeg,image/png,image/webp"
              className="hidden"
              onChange={(e) => pickFile(e.target.files?.[0] || null)}
            />
          </label>

          {previewUrl && (
            /* eslint-disable-next-line @next/next/no-img-element */
            <img src={previewUrl} alt="Receipt preview" className="max-h-72 w-full rounded-lg border border-[#eef1ed] object-contain" />
          )}

          <button className="btn-primary w-full py-2.5" onClick={handleScan} disabled={!file || scanning}>
            <ScanLine size={16} />
            {scanning ? "Reading receipt..." : "Scan receipt"}
          </button>

          {scanError && <p className="rounded-lg bg-red-50 p-3 text-sm text-red-700">{scanError}</p>}

          <div className="flex items-start gap-2 rounded-lg bg-[#fff8e8] p-3 text-xs text-[#8a6d1f]">
            <AlertTriangle size={15} className="mt-0.5 shrink-0" />
            <p>
              OCR is not perfect, especially on handwritten or faded receipts. Check every line before saving — nothing is
              recorded until you confirm.
            </p>
          </div>
        </section>

        <section className="space-y-6">
          {!scan ? (
            <div className="panel p-10 text-center text-sm text-[#718078]">
              Scan a receipt to review its line items here.
            </div>
          ) : (
            <>
              <div className="panel space-y-2 p-5">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <p className="eyebrow">2. Review &amp; correct</p>
                  <span className="rounded-full bg-[#eef1f4] px-2.5 py-1 text-[11px] font-semibold uppercase tracking-wide text-[#52616a]">
                    {scan.confidence || "unknown"} confidence
                  </span>
                </div>
                <p className="text-sm text-[#526057]">
                  {scan.merchant || "Unknown merchant"}
                  {scan.date ? ` · ${scan.date}` : ""}
                </p>
                {scan.notes && <p className="text-xs text-[#8a958d]">{scan.notes}</p>}
              </div>

              <div className="panel space-y-3 p-5">
                <div className="hidden gap-3 text-xs font-semibold uppercase tracking-wide text-[#8a958d] sm:grid sm:grid-cols-[1.6fr_.7fr_.8fr_.8fr_auto]">
                  <span>Product</span>
                  <span>Qty</span>
                  <span>Unit price</span>
                  <span className="text-right">Subtotal</span>
                  <span />
                </div>

                {lines.map((line, index) => (
                  <div
                    key={index}
                    className="grid gap-3 rounded-lg border border-[#eef1ed] p-3 sm:grid-cols-[1.6fr_.7fr_.8fr_.8fr_auto] sm:items-center"
                  >
                    <div className="space-y-1">
                      <select
                        className="field"
                        value={line.productId}
                        onChange={(e) => updateLine(index, "productId", e.target.value)}
                      >
                        <option value="">Select a product...</option>
                        {products.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.name}
                          </option>
                        ))}
                      </select>
                      {line.description && (
                        <p className="text-xs text-[#8a958d]">Read as: “{line.description}”</p>
                      )}
                    </div>
                    <input
                      className="field"
                      type="number"
                      min={1}
                      value={line.quantity}
                      onChange={(e) => updateLine(index, "quantity", e.target.value)}
                    />
                    <input
                      className="field"
                      type="number"
                      min={0}
                      step="0.01"
                      value={line.unitPrice}
                      onChange={(e) => updateLine(index, "unitPrice", e.target.value)}
                    />
                    <p className="text-sm font-semibold sm:text-right">
                      {formatMoney(inputToCents(line.unitPrice) * (Number(line.quantity) || 0))}
                    </p>
                    <button
                      type="button"
                      onClick={() => removeLine(index)}
                      className="justify-self-start rounded-md p-1.5 text-[#a3543f] hover:bg-[#fdeeea]"
                      aria-label="Remove line"
                    >
                      <Trash2 size={15} />
                    </button>
                  </div>
                ))}

                <button type="button" className="btn-secondary" onClick={addLine}>
                  <Plus size={15} /> Add line
                </button>
              </div>

              <div className="panel space-y-5 p-5">
                <p className="eyebrow">3. Save as</p>
                <div className="grid grid-cols-2 gap-2">
                  {(["purchase", "sale"] as RecordType[]).map((t) => (
                    <button
                      key={t}
                      type="button"
                      onClick={() => setRecordType(t)}
                      className={`rounded-lg border px-3 py-2.5 text-sm font-semibold capitalize transition ${
                        recordType === t
                          ? "border-[#16794c] bg-[#e7f4ec] text-[#12633e]"
                          : "border-[#dce7de] text-[#647168] hover:bg-[#f7f9f6]"
                      }`}
                    >
                      {t}
                    </button>
                  ))}
                </div>

                {recordType === "purchase" ? (
                  <div className="space-y-3">
                    <div>
                      <label className="mb-1.5 block text-sm font-semibold">Supplier</label>
                      <select className="field" value={supplierId} onChange={(e) => setSupplierId(e.target.value)}>
                        <option value="">Select a supplier...</option>
                        {suppliers.map((s) => (
                          <option key={s.id} value={s.id}>
                            {s.name}
                          </option>
                        ))}
                      </select>
                    </div>
                    <div>
                      <label className="mb-1.5 block text-sm font-semibold">Amount paid (optional)</label>
                      <input
                        className="field"
                        type="number"
                        min={0}
                        step="0.01"
                        placeholder="0.00"
                        value={amountPaid}
                        onChange={(e) => setAmountPaid(e.target.value)}
                      />
                    </div>
                  </div>
                ) : (
                  <div className="space-y-3">
                    <div>
                      <label className="mb-1.5 block text-sm font-semibold">Sale type</label>
                      <select className="field" value={saleType} onChange={(e) => setSaleType(e.target.value as SaleType)}>
                        <option value="cash">Cash sale</option>
                        <option value="credit">Credit sale</option>
                        <option value="quotation">Quotation</option>
                      </select>
                    </div>
                    <div>
                      <label className="mb-1.5 block text-sm font-semibold">Customer (optional)</label>
                      <select className="field" value={customerId} onChange={(e) => setCustomerId(e.target.value)}>
                        <option value="">Walk-in customer</option>
                        {customers.map((c) => (
                          <option key={c.id} value={c.id}>
                            {c.name}
                          </option>
                        ))}
                      </select>
                    </div>
                    <p className="text-xs text-[#8a958d]">
                      Sale prices come from your product catalogue, so the final total may differ from the receipt.
                    </p>
                  </div>
                )}

                <div>
                  <label className="mb-1.5 block text-sm font-semibold">Note (optional)</label>
                  <input className="field" value={note} onChange={(e) => setNote(e.target.value)} />
                </div>

                <div className="flex items-center justify-between border-t border-[#eef1ed] pt-4 text-sm">
                  <span className="text-[#718078]">Reviewed total</span>
                  <span className="text-xl font-bold">{formatMoney(estimatedTotal)}</span>
                </div>

                {commitError && <p className="rounded-lg bg-red-50 p-3 text-sm text-red-700">{commitError}</p>}

                <button className="btn-primary w-full py-3" onClick={handleCommit} disabled={!reviewReady || committing}>
                  {committing ? "Saving..." : `Confirm & save ${recordType}`}
                </button>
                {!reviewReady && (
                  <p className="text-xs text-[#8a958d]">
                    Every line needs a product and a quantity{recordType === "purchase" ? ", and a supplier must be selected" : ""}.
                  </p>
                )}
              </div>
            </>
          )}
        </section>
      </div>
    </>
  );
}
