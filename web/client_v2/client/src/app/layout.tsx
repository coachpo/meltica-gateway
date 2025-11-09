import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import { Nav } from "@/components/nav";
import { ClientProviders } from "@/components/providers/client-providers";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Meltica Control - Trading Strategy Management",
  description: "Control plane for Meltica trading gateway",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body
        className={`${geistSans.variable} ${geistMono.variable} antialiased`}
      >
        <ClientProviders>
          <Nav />
          <main className="relative mx-auto w-full max-w-7xl px-6 py-8 sm:px-8 lg:px-10">
            <div className="space-y-8 rounded-[2.5rem] border border-border/30 bg-background/70 p-6 shadow-[0_60px_120px_-80px_rgba(15,23,42,0.8)] backdrop-blur-2xl sm:p-8 lg:p-10">
              {children}
            </div>
          </main>
        </ClientProviders>
      </body>
    </html>
  );
}
