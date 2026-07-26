import type { Metadata } from 'next';
import AuthPathView from './auth-path-view';

export const metadata: Metadata = {
  title: 'OpenTela Account',
};

// The auth UI links to its own routes under the /auth base path (sign-up,
// forgot-password, reset-password, magic-link, …). This catch-all renders
// whichever view the library asked for, so those links resolve instead of 404ing.
export default async function AuthPathPage({
  params,
}: {
  params: Promise<{ pathname: string }>;
}) {
  const { pathname } = await params;
  return <AuthPathView pathname={pathname} />;
}
