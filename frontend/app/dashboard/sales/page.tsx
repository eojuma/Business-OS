"use client";

import { useEffect, useMemo, useState } from "react";
import { api } from "@/lib/api";
import { PageHeader, formatMoney } from "@/components/app-shell";
import { QRCodeSVG } from "qrcode.react";
import { Check, Minus, Plus, Printer, Search, Trash2 } from "lucide-react";

interface Product {
  id: string;
  name: string;
  unit: string;
  price: number; // cents
}

interface Customer {
  id: string;
  name: string;
  phone?: string;
  email?: string;
}

interface Business {
  name: string;
  phone?: string;
  email?: string;
  address?: string;
  currency?: string;
}

interface LineItem {
  productId: string;
  quantity: number;
}

type SaleType = "cash" | "credit" | "quotation";

interface RecordedSale {
  id: string;
  sale_type: SaleType;
  total_amount: number;
  discount: number;
  created_at: string;
  line_items?: { product_id: string; quantity: number; unit_price: number; subtotal: number }[];
}

const SALE_TYPE_LABEL: Record<SaleType, string> = {
  cash: "Cash sale",
  credit: "Credit sale",
  quotation: "Quotation",
};

export default function SalesPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [business, setBusiness] = useState<Business | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [query, setQuery] = useState("");
  const [items, setItems] = useState<LineItem[]>([]);
  const [saleType, setSaleType] = useState<SaleType>("cash");
  const [customerId, setCustomerId] = useState("");
  const [note, setNote] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState("");
  const [receipt, setReceipt] = useState<RecordedSale | null>(null);
  const [receiptCustomer, setReceiptCustomer] = useState<Customer | null>(null);

  useEffect(() => {
    async function load() {
      try {
        const [productsRes, customersRes, businessRes] = await Promise.all([
          api.get("/products"),
          api.get("/customers"),
          api.get("/business"),
        ]);
        setProducts(productsRes.data.data || []);
        setCustomers(customersRes.data.data || []);
        setBusiness(businessRes.data.data || null);
      } catch (err: any) {
        setError(err.response?.data?.error || "Failed to load sales page");
      } finally {
        setLoading(false);
      }
    }
    load();
  }, []);

  const filteredProducts = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return products.slice(0, 8);
    return products.filter((p) => p.name.toLowerCase().includes(q)).slice(0, 8);
  }, [products, query]);

  const productById = useMemo(() => {
    const map: Record<string, Product> = {};
    products.forEach((p) => (map[p.id] = p));
    return map;
  }, [products]);

  const selectedCustomer = customers.find((c) => c.id === customerId) || null;

  function addProduct(productId: string) {
    setItems((prev) => {
      const existing = prev.find((i) => i.productId === productId);
      if (existing) {
        return prev.map((i) => (i.productId === productId ? { ...i, quantity: i.quantity + 1 } : i));
      }
      return [...prev, { productId, quantity: 1 }];
    });
    setQuery("");
  }

  function setQuantity(productId: string, quantity: number) {
    if (quantity <= 0) {
      setItems((prev) => prev.filter((i) => i.productId !== productId));
      return;
    }
    setItems((prev) => prev.map((i) => (i.productId === productId ? { ...i, quantity } : i)));
  }

  function removeItem(productId: string) {
    setItems((prev) => prev.filter((i) => i.productId !== productId));
  }

  const estimatedTotal = items.reduce((sum, item) => {
    const product = productById[item.productId];
    if (!product) return sum;
    return sum + product.price * item.quantity;
  }, 0);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError("");

    if (items.length === 0) {
      setFormError("Add at least one product");
      return;
    }
    if (saleType === "credit" && !customerId) {
      setFormError("A credit sale needs a customer");
      return;
    }

    setSubmitting(true);
    try {
      const payload: any = {
        sale_type: saleType,
        note,
        items: items.map((i) => ({ product_id: i.productId, quantity: i.quantity })),
      };
      if (customerId) payload.customer_id = customerId;

      const res = await api.post("/sales", payload);
      setReceiptCustomer(selectedCustomer);
      setReceipt(res.data.data as RecordedSale);
      setItems([]);
      setCustomerId("");
      setNote("");
    } catch (err: any) {
      setFormError(err.response?.data?.error || "Failed to record sale");
    } finally {
      setSubmitting(false);
    }
  }

  if (loading) return <main className="p-8">Loading...</main>;
  if (error) return <main className="p-8 text-red-600">{error}</main>;

  if (receipt) {
    return <ReceiptView receipt={receipt} business={business} customer={receiptCustomer} products={productById} onNew={() => setReceipt(null)} />;
  }

  return (
    <>
      <PageHeader
        eyebrow="Operations"
        title="Record a sale"
        description="Search products, choose how the sale is settled, and issue a receipt."
      />

      <form onSubmit={handleSubmit} className="grid gap-6 xl:grid-cols-[1.4fr_.6fr]">
        <section className="panel space-y-5 p-5">
          {formError && <p className="rounded-lg bg-red-50 p-3 text-sm text-red-700">{formError}</p>}

          <div>
            <label className="mb-1.5 block text-sm font-semibold">Find a product</label>
            <div className="relative">
              <Search size={16} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[#9aa59d]" />
              <input
                className="field pl-9"
                placeholder="Search by product name"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </div>
            {query && (
              <div className="mt-2 max-h-64 overflow-y-auto rounded-lg border border-[#e5e9e4] bg-white">
                {filteredProducts.length === 0 ? (
                  <p className="p-3 text-sm text-[#718078]">No products match “{query}”.</p>
                ) : (
                  filteredProducts.map((p) => (
                    <button
                      key={p.id}
                      type="button"
                      onClick={() => addProduct(p.id)}
                      className="flex w-full items-center justify-between border-b border-[#f1f4f0] px-3 py-2.5 text-left text-sm last:border-0 hover:bg-[#f7f9f6]"
                    >
                      <span className="font-medium">{p.name}</span>
                      <span className="text-[#718078]">
                        {formatMoney(p.price)}/{p.unit}
                      </span>
                    </button>
                  ))
                )}
              </div>
            )}
          </div>

          <div>
            <p className="eyebrow mb-2">Line items</p>
            {items.length === 0 ? (
              <p className="rounded-lg border border-dashed border-[#cfd8d0] p-6 text-center text-sm text-[#718078]">
                No products added yet. Search above to add one.
              </p>
            ) : (
              <div className="space-y-2">
                {items.map((item) => {
                  const product = productById[item.productId];
                  if (!product) return null;
                  return (
                    <div key={item.productId} className="flex items-center gap-3 rounded-lg border border-[#eef1ed] p-3">
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-semibold">{product.name}</p>
                        <p className="text-xs text-[#718078]">
                          {formatMoney(product.price)}/{product.unit}
                        </p>
                      </div>
                      <div className="flex items-center gap-1">
                        <button type="button" className="rounded-md border border-[#dce7de] p-1.5 text-[#526057] hover:bg-[#f2f5f1]" onClick={() => setQuantity(item.productId, item.quantity - 1)} aria-label="Decrease">
                          <Minus size={14} />
                        </button>
                        <input
                          type="number"
                          min={1}
                          value={item.quantity}
                          onChange={(e) => setQuantity(item.productId, parseInt(e.target.value, 10) || 0)}
                          className="w-16 rounded-md border border-[#dce7de] px-2 py-1.5 text-center text-sm"
                        />
                        <button type="button" className="rounded-md border border-[#dce7de] p-1.5 text-[#526057] hover:bg-[#f2f5f1]" onClick={() => setQuantity(item.productId, item.quantity + 1)} aria-label="Increase">
                          <Plus size={14} />
                        </button>
                      </div>
                      <p className="w-24 text-right text-sm font-semibold">{formatMoney(product.price * item.quantity)}</p>
                      <button type="button" onClick={() => removeItem(item.productId)} className="rounded-md p-1.5 text-[#a3543f] hover:bg-[#fdeeea]" aria-label="Remove">
                        <Trash2 size={15} />
                      </button>
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          <div>
            <label className="mb-1.5 block text-sm font-semibold">Note (optional)</label>
            <input className="field" placeholder="e.g. Delivery to site on Friday" value={note} onChange={(e) => setNote(e.target.value)} />
          </div>
        </section>

        <aside className="panel h-fit space-y-5 p-5">
          <div>
            <p className="eyebrow mb-2">Sale type</p>
            <div className="grid grid-cols-3 gap-2">
              {(["cash", "credit", "quotation"] as SaleType[]).map((t) => (
                <button
                  key={t}
                  type="button"
                  onClick={() => setSaleType(t)}
                  className={`rounded-lg border px-2 py-2.5 text-xs font-semibold capitalize transition ${
                    saleType === t ? "border-[#16794c] bg-[#e7f4ec] text-[#12633e]" : "border-[#dce7de] text-[#647168] hover:bg-[#f7f9f6]"
                  }`}
                >
                  {t}
                </button>
              ))}
            </div>
            <p className="mt-2 text-xs text-[#8a958d]">
              {saleType === "cash" && "Cash sale — stock is reduced, nothing goes on account."}
              {saleType === "credit" && "Credit sale — stock is reduced and the balance is added to the customer's account."}
              {saleType === "quotation" && "Quotation — a priced document only. Stock and accounts are not affected."}
            </p>
          </div>

          <div>
            <label className="mb-1.5 block text-sm font-semibold">
              Customer {saleType === "credit" ? <span className="text-[#a3543f]">(required)</span> : <span className="font-normal text-[#8a958d]">(optional)</span>}
            </label>
            <select className="field" value={customerId} onChange={(e) => setCustomerId(e.target.value)}>
              <option value="">Walk-in customer</option>
              {customers.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
            {selectedCustomer && (
              <div className="mt-2 rounded-lg bg-[#f7f9f6] p-3 text-xs text-[#718078]">
                <p className="font-semibold text-[#17211b]">{selectedCustomer.name}</p>
                {selectedCustomer.phone && <p className="mt-0.5">{selectedCustomer.phone}</p>}
                {selectedCustomer.email && <p className="mt-0.5">{selectedCustomer.email}</p>}
              </div>
            )}
          </div>

          <div className="border-t border-[#eef1ed] pt-4">
            <div className="flex items-center justify-between text-sm">
              <span className="text-[#718078]">Estimated total</span>
              <span className="text-xl font-bold">{formatMoney(estimatedTotal)}</span>
            </div>
            <p className="mt-1 text-xs text-[#8a958d]">Final total is confirmed when you save.</p>
          </div>

          <button type="submit" disabled={submitting} className="btn-primary w-full py-3">
            {submitting ? "Recording..." : saleType === "quotation" ? "Create quotation" : "Complete sale"}
          </button>
        </aside>
      </form>
    </>
  );
}

function ReceiptView({
  receipt,
  business,
  customer,
  products,
  onNew,
}: {
  receipt: RecordedSale;
  business: Business | null;
  customer: Customer | null;
  products: Record<string, Product>;
  onNew: () => void;
}) {
  const type = (receipt.sale_type || "cash") as SaleType;
  const shortId = receipt.id.slice(0, 8).toUpperCase();
  const created = new Date(receipt.created_at);

  const qrValue = [
    `Business OS ${SALE_TYPE_LABEL[type]}`,
    business?.name ? `Business: ${business.name}` : "",
    `Ref: ${shortId}`,
    customer?.name ? `Customer: ${customer.name}` : "Customer: Walk-in",
    `Total: ${formatMoney(receipt.total_amount)}`,
    created.toLocaleString(),
  ]
    .filter(Boolean)
    .join("\n");

  return (
    <>
      <style>{`
        @media print {
          body * { visibility: hidden; }
          #receipt-print, #receipt-print * { visibility: visible; }
          #receipt-print { position: absolute; left: 0; top: 0; width: 100%; box-shadow: none; border: none; }
          .no-print { display: none !important; }
        }
      `}</style>

      <div className="no-print mb-6 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2 text-[#16794c]">
          <Check size={18} />
          <p className="font-semibold">{SALE_TYPE_LABEL[type]} recorded</p>
        </div>
        <div className="flex gap-2">
          <button className="btn-secondary" onClick={onNew}>
            <Plus size={15} /> New sale
          </button>
          <button className="btn-primary" onClick={() => window.print()}>
            <Printer size={15} /> Print / Save as PDF
          </button>
        </div>
      </div>

      <div id="receipt-print" className="panel mx-auto max-w-xl p-8">
        <div className="flex items-start justify-between border-b border-[#eef1ed] pb-4">
          <div>
            <p className="text-xl font-bold tracking-tight">{business?.name || "Business OS"}</p>
            {business?.address && <p className="mt-1 text-xs text-[#718078]">{business.address}</p>}
            {business?.phone && <p className="text-xs text-[#718078]">{business.phone}</p>}
            {business?.email && <p className="text-xs text-[#718078]">{business.email}</p>}
          </div>
          <div className="text-right">
            <p className="text-sm font-bold uppercase tracking-wide text-[#16794c]">{SALE_TYPE_LABEL[type]}</p>
            <p className="mt-1 text-xs text-[#718078]">Ref {shortId}</p>
            <p className="text-xs text-[#718078]">{created.toLocaleString()}</p>
          </div>
        </div>

        <div className="border-b border-[#eef1ed] py-4">
          <p className="eyebrow mb-1">Billed to</p>
          {customer ? (
            <>
              <p className="text-sm font-semibold">{customer.name}</p>
              {customer.phone && <p className="text-xs text-[#718078]">{customer.phone}</p>}
              {customer.email && <p className="text-xs text-[#718078]">{customer.email}</p>}
            </>
          ) : (
            <p className="text-sm text-[#718078]">Walk-in customer</p>
          )}
        </div>

        <table className="w-full py-4 text-sm">
          <thead>
            <tr className="text-left text-xs uppercase text-[#718078]">
              <th className="py-2">Item</th>
              <th className="py-2 text-right">Qty</th>
              <th className="py-2 text-right">Unit</th>
              <th className="py-2 text-right">Amount</th>
            </tr>
          </thead>
          <tbody>
            {(receipt.line_items || []).map((li, idx) => {
              const product = products[li.product_id];
              return (
                <tr key={idx} className="border-t border-[#f1f4f0]">
                  <td className="py-2">{product?.name || "Item"}</td>
                  <td className="py-2 text-right">{li.quantity}</td>
                  <td className="py-2 text-right">{formatMoney(li.unit_price)}</td>
                  <td className="py-2 text-right">{formatMoney(li.subtotal)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>

        <div className="mt-4 flex items-center justify-between border-t border-[#eef1ed] pt-4">
          <div>
            <div className="flex h-24 w-24 items-center justify-center rounded-lg border border-[#eef1ed] p-1">
              <QRCodeSVG value={qrValue} size={88} />
            </div>
            <p className="mt-1 text-[10px] text-[#8a958d]">Scan to verify</p>
          </div>
          <div className="text-right">
            <p className="text-xs uppercase text-[#718078]">Total</p>
            <p className="text-2xl font-bold">{formatMoney(receipt.total_amount)}</p>
            {type === "quotation" && <p className="mt-1 text-xs text-[#8a958d]">Quotation valid for 14 days</p>}
            {type === "credit" && <p className="mt-1 text-xs text-[#8a958d]">Charged to customer account</p>}
          </div>
        </div>

        <p className="mt-6 border-t border-[#eef1ed] pt-4 text-center text-xs text-[#8a958d]">
          Thank you for your business.
        </p>
      </div>
    </>
  );
}
