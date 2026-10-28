import type { Metadata } from "next";
import Script from "next/script";
import "./globals.css";
import { ThemeProvider } from "@/lib/theme-provider";

export const metadata: Metadata = {
  title: {
    default: "Business OS | Run your store with clarity",
    template: "%s | Business OS",
  },
  description: "Inventory, sales, customers, suppliers and financial insights for growing African hardware stores.",
};

// Runs before first paint so the stored/system theme is applied without a
// flash of the wrong theme. Kept tiny and dependency-free on purpose.
const themeScript = `(function(){try{var s=localStorage.getItem('theme');var d=s==='dark'||((!s||s==='system')&&window.matchMedia('(prefers-color-scheme: dark)').matches);var r=document.documentElement;r.classList.toggle('dark',d);r.style.colorScheme=d?'dark':'light';}catch(e){}})();`;

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body className="antialiased">
        <ThemeProvider>
          {children}
          {/* Google Identity Services is loaded once for the whole app; the
              sign-in component only waits for window.google to appear. */}
          <Script
            id="google-identity-services"
            src="https://accounts.google.com/gsi/client"
            strategy="afterInteractive"
          />
        </ThemeProvider>
      </body>
    </html>
  );
}
