export function resolveAdminHomePath(isSimpleMode: boolean): string {
  return isSimpleMode ? '/admin/settings?tab=users' : '/admin/dashboard'
}

export function resolveCompletedSetupRedirectPath(
  isAuthenticated: boolean,
  isAdmin: boolean,
  isSimpleMode = false,
): string {
  if (!isAuthenticated) {
    return '/login?redirect=/admin/settings%3Ftab%3Dusers'
  }

  return isAdmin ? resolveAdminHomePath(isSimpleMode) : '/dashboard'
}
