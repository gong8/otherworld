import type { Metadata, Viewport } from "next";
import localFont from "next/font/local";
import "./globals.css";

const garamond = localFont({
  src: [
    { path: "../assets/fonts/eb-garamond-400-normal.woff2", weight: "400", style: "normal" },
    { path: "../assets/fonts/eb-garamond-500-normal.woff2", weight: "500", style: "normal" },
    { path: "../assets/fonts/eb-garamond-400-italic.woff2", weight: "400", style: "italic" },
    { path: "../assets/fonts/eb-garamond-500-italic.woff2", weight: "500", style: "italic" },
  ],
  display: "swap",
  variable: "--font-garamond",
});

export const metadata: Metadata = {
  title: "the otherworld",
  description: "the world beside the world.",
};

export const viewport: Viewport = {
  themeColor: "#ECE9E1",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={garamond.variable}>
      <body>{children}</body>
    </html>
  );
}
