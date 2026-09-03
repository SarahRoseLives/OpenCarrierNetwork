import 'package:flutter_test/flutter_test.dart';
import 'package:ocnphone/main.dart';

void main() {
  testWidgets('App should render', (WidgetTester tester) async {
    await tester.pumpWidget(const OcnSoftphoneApp());
    expect(find.text('OCN Softphone'), findsOneWidget);
  });
}
