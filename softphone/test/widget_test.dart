import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ocnphone/main.dart';

void main() {
  testWidgets('App boots to the initializing spinner', (WidgetTester tester) async {
    await tester.pumpWidget(const OcnSoftphoneApp());
    // Before storage decides whether a kSIM exists, the app shows its
    // "uninitialized" progress indicator.
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    expect(find.byType(MaterialApp), findsOneWidget);
  });
}
