/**
 * OpenTela Ownership Tools – Logging Utilities
 *
 * Provides structured, color-coded console output with a debug toggle
 * controlled by the DEBUG environment variable.
 */

import chalk from "chalk";

// ---------------------------------------------------------------------------
// Debug flag
// ---------------------------------------------------------------------------

let _debug =
  (process.env.DEBUG ?? "").trim().toLowerCase() === "true" ||
  process.env.DEBUG === "1";

/**
 * Enable or disable debug logging at runtime.
 */
export function setDebug(enabled: boolean): void {
  _debug = enabled;
}

/**
 * Returns the current debug state.
 */
export function isDebug(): boolean {
  return _debug;
}

// ---------------------------------------------------------------------------
// Log levels
// ---------------------------------------------------------------------------

/**
 * Informational message – always shown.
 */
export function info(...args: unknown[]): void {
  console.log(chalk.blue("ℹ"), ...args);
}

/**
 * Success message – always shown.
 */
export function success(...args: unknown[]): void {
  console.log(chalk.green("✔"), ...args);
}

/**
 * Warning message – always shown.
 */
export function warn(...args: unknown[]): void {
  console.warn(chalk.yellow("⚠"), ...args);
}

/**
 * Error message – always shown.
 */
export function error(...args: unknown[]): void {
  console.error(chalk.red("✖"), ...args);
}

/**
 * Debug message – only shown when debug mode is enabled.
 */
export function debug(...args: unknown[]): void {
  if (_debug) {
    console.log(chalk.gray("⊡"), chalk.gray("[debug]"), ...args);
  }
}

/**
 * Print a labeled key-value pair, nicely aligned.
 *
 * @param label - The label / key (left column).
 * @param value - The value (right column).
 * @param indent - Number of leading spaces (default 2).
 */
export function kv(label: string, value: unknown, indent = 2): void {
  const pad = " ".repeat(indent);
  const labelStr = chalk.dim(`${label}:`);
  console.log(`${pad}${labelStr.padEnd(24 + indent)} ${value}`);
}

/**
 * Print a section header.
 */
export function header(title: string): void {
  console.log();
  console.log(chalk.bold.underline(title));
  console.log();
}

/**
 * Print a horizontal divider.
 */
export function divider(char = "─", width = 60): void {
  console.log(chalk.dim(char.repeat(width)));
}

/**
 * Print a blank line.
 */
export function blank(): void {
  console.log();
}

/**
 * Print a step counter, e.g. "  [1/4] Fetching blockhash..."
 */
export function step(current: number, total: number, message: string): void {
  const tag = chalk.cyan(`[${current}/${total}]`);
  console.log(`  ${tag} ${message}`);
}

/**
 * Format a Solana explorer URL for a transaction signature.
 */
export function explorerTxUrl(
  signature: string,
  cluster: string = "devnet",
): string {
  const base = "https://explorer.solana.com/tx";
  const suffix =
    cluster === "mainnet-beta" || cluster === "mainnet"
      ? ""
      : `?cluster=${cluster}`;
  return `${base}/${signature}${suffix}`;
}

/**
 * Format a Solana explorer URL for an account / address.
 */
export function explorerAddressUrl(
  address: string,
  cluster: string = "devnet",
): string {
  const base = "https://explorer.solana.com/address";
  const suffix =
    cluster === "mainnet-beta" || cluster === "mainnet"
      ? ""
      : `?cluster=${cluster}`;
  return `${base}/${address}${suffix}`;
}

/**
 * Convenience: log a transaction result with explorer link.
 */
export function logTx(
  label: string,
  signature: string,
  cluster: string = "devnet",
): void {
  success(`${label}`);
  kv("Signature", signature);
  kv("Explorer", explorerTxUrl(signature, cluster));
}

/**
 * Convenience: log a fatal error and exit the process.
 */
export function fatal(message: string, exitCode = 1): never {
  error(message);
  process.exit(exitCode);
}
