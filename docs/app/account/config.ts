import { TOKEN_PROGRAM_ID } from '@solana/spl-token';
import { PublicKey } from '@solana/web3.js';

const DEFAULT_MINT = 'EsmcTrdLkFqV3mv4CjLF3AmCx132ixfFSYYRWD78cDzR';
const DEFAULT_REWARD_PROGRAM_ID = 'HMSLAjUbu7XkMmw74fmTMoksg2TSaatDYZ8VjQUU4HdE';

function readNumber(value: string | undefined, fallback: number): number {
  if (!value) return fallback;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function readPublicKey(value: string | undefined, fallback: PublicKey): PublicKey {
  if (!value) return fallback;
  try {
    return new PublicKey(value);
  } catch {
    return fallback;
  }
}

function readOptionalPublicKey(value: string | undefined): PublicKey | null {
  if (!value) return null;
  try {
    return new PublicKey(value);
  } catch {
    return null;
  }
}

export const tokenManagerConfig = {
  authApiBaseUrl:
    process.env.NEXT_PUBLIC_AUTH_API_BASE_URL?.replace(/\/+$/, '') ??
    'http://localhost:8090',
  apiBaseUrl:
    process.env.NEXT_PUBLIC_API_BASE_URL?.replace(/\/+$/, '') ??
    'https://api.opentela.ai',
  neonAuthUrl:
    process.env.NEXT_PUBLIC_NEON_AUTH_URL?.replace(/\/+$/, '') ?? '',
  // Not api.mainnet-beta.solana.com: that endpoint answers CORS preflight but
  // rejects the request itself with 403 as soon as it carries a browser Origin,
  // so every balance lookup from this page fails. This one allows browser
  // traffic. It is a shared public endpoint — point NEXT_PUBLIC_SOLANA_RPC_URL
  // at a dedicated provider for anything beyond light use.
  solanaRpcUrl:
    process.env.NEXT_PUBLIC_SOLANA_RPC_URL ??
    'https://solana-rpc.publicnode.com',
  solanaCluster: process.env.NEXT_PUBLIC_SOLANA_CLUSTER ?? 'mainnet-beta',
  otelaMint: readPublicKey(
    process.env.NEXT_PUBLIC_OTELA_MINT,
    new PublicKey(DEFAULT_MINT),
  ),
  otelaDecimals: readNumber(process.env.NEXT_PUBLIC_OTELA_DECIMALS, 9),
  tokenProgramId: readPublicKey(
    process.env.NEXT_PUBLIC_TOKEN_PROGRAM_ID,
    TOKEN_PROGRAM_ID,
  ),
  rewardProgramId: readPublicKey(
    process.env.NEXT_PUBLIC_REWARD_PROGRAM_ID,
    new PublicKey(DEFAULT_REWARD_PROGRAM_ID),
  ),
  rewardCampaign: readOptionalPublicKey(
    process.env.NEXT_PUBLIC_REWARD_CAMPAIGN,
  ),
};
