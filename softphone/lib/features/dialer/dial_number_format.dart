/// Formats a phone number as you dial it.
///
/// Local numbers (no area code yet) read `xxx-xxxx`; once a 10-digit full
/// number is possible (area code + local) it reads `xxx-xxx-xxxx`. Service
/// codes like `*01` and anything containing `*`/`#` are returned untouched.
String formatDialNumber(String raw) {
  if (raw.isEmpty) return raw;
  if (raw.contains('*') || raw.contains('#')) return raw;

  if (raw.length <= 3) return raw;
  if (raw.length <= 7) {
    return '${raw.substring(0, 3)}-${raw.substring(3)}';
  }
  if (raw.length <= 10) {
    return '${raw.substring(0, 3)}-${raw.substring(3, 6)}-${raw.substring(6)}';
  }
  return raw;
}
