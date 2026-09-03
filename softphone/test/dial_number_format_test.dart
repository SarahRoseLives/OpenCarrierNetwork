import 'package:flutter_test/flutter_test.dart';
import 'package:ocnphone/features/dialer/dial_number_format.dart';

void main() {
  test('local number (no area code) formats as xxx-xxxx', () {
    expect(formatDialNumber('1234567'), '123-4567');
    expect(formatDialNumber('123'), '123');
    expect(formatDialNumber('1234'), '123-4');
    expect(formatDialNumber(''), '');
  });

  test('full 10-digit number formats as xxx-xxx-xxxx', () {
    expect(formatDialNumber('1234567890'), '123-456-7890');
    expect(formatDialNumber('12345678'), '123-456-78');
    expect(formatDialNumber('2025550123'), '202-555-0123');
  });

  test('service codes with * and # are left untouched', () {
    expect(formatDialNumber('*01'), '*01');
    expect(formatDialNumber('*86#'), '*86#');
    expect(formatDialNumber('123*4'), '123*4');
  });
}
