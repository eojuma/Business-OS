import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: {
    default: "Business OS | Run your store with clarity",
    template: "%s | Business OS",
  },
  description: "Inventory, sales, customers, suppliers and financial insights for growing African hardware stores.",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body className="antialiased">{children}</body>
    </html>
  );
}
