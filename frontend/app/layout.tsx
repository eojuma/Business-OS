import type { Metadata } from "next";
import Script from "next/script";
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
      <body className="antialiased">
        {children}
        {/* Google Identity Services is loaded once for the whole app; the
            sign-in component only waits for window.google to appear. */}
        <Script
          id="google-identity-services"
          src="https://accounts.google.com/gsi/client"
          strategy="afterInteractive"
        />
      </body>
    </html>
  );
}   