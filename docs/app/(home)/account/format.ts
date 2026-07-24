export function middleEllipsis(value: string, prefix = 6, suffix = 6): string {
  if (value.length <= prefix + suffix + 3) return value;
  return `${value.slice(0, prefix)}...${value.slice(-suffix)}`;
}

export function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date);
}

export function rawToUiAmount(raw: bigint, decimals: number): string {
  const scale = 10n ** BigInt(decimals);
  const whole = raw / scale;
  const fraction = raw % scale;
  if (fraction === 0n) return whole.toString();
  const padded = fraction.toString().padStart(decimals, '0').replace(/0+$/, '');
  return `${whole.toString()}.${padded}`;
}

export function uiAmountToRaw(value: string, decimals: number): bigint {
  const trimmed = value.trim();
  if (!/^\d+(\.\d+)?$/.test(trimmed)) {
    throw new Error('Enter a positive token amount');
  }

  const [wholePart, fractionPart = ''] = trimmed.split('.');
  if (fractionPart.length > decimals) {
    throw new Error(`OTELA supports at most ${decimals} decimal places`);
  }

  const whole = BigInt(wholePart);
  const fraction = BigInt((fractionPart || '0').padEnd(decimals, '0'));
  const raw = whole * 10n ** BigInt(decimals) + fraction;
  if (raw <= 0n) throw new Error('Amount must be greater than zero');
  return raw;
}
