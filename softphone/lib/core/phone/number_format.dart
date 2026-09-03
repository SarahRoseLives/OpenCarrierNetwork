// Phone-number helpers for storage keys, display, and dialing.
//
// OCN numbers are either 7-digit local (`5551234`), 10-digit full
// (`4405551234`), or a service code containing `*`/`#` (e.g. `*01`).
//
// Matching and storage use the *canonical* form: digits only, with a 7-digit
// local number promoted to full by prepending the owner's area code (the same
// rule the server applies when parsing a destination).

/// True for service codes / DTMF strings (`*01`, `#123`).
bool isServiceCode(String raw) {
  return raw.contains('*') || raw.contains('#');
}

String _digits(String s) => s.replaceAll(RegExp(r'[^0-9]'), '');

/// Canonical storage/matching key for a dialed number or a caller number.
///
/// 10-digit numbers pass through unchanged; 7-digit numbers are promoted to
/// full using [ownArea] (when known); service codes are kept as typed; anything
/// else is reduced to digits.
String canonicalNumber(String raw, {String? ownArea}) {
  final trimmed = raw.trim();
  if (trimmed.isEmpty) return '';
  if (isServiceCode(trimmed)) return trimmed;
  final digits = _digits(trimmed);
  if (digits.length == 7 && ownArea != null && ownArea.isNotEmpty) {
    return ownArea + digits;
  }
  return digits;
}

/// User-friendly rendering of a canonical number or service code.
String displayNumber(String canonical) {
  final c = canonical.trim();
  if (c.isEmpty) return c;
  if (isServiceCode(c)) return c;
  if (c.length == 10) {
    return '${c.substring(0, 3)}-${c.substring(3, 6)}-${c.substring(6)}';
  }
  if (c.length == 7) {
    return '${c.substring(0, 3)}-${c.substring(3)}';
  }
  return c;
}

/// What to feed [AppState.makeCall] for a stored canonical number.
String dialFromCanonical(String canonical) {
  return canonical.trim();
}
