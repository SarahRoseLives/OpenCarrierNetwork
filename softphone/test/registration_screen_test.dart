import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:provider/provider.dart';
import 'package:ocnphone/core/app_state.dart';
import 'package:ocnphone/features/registration/registration_screen.dart';

void main() {
  const sampleUrl =
      'ocnksim://127.0.0.1/?name=Rose&server=ws%3A%2F%2F127.0.0.1%3A9100%2Fws&token=abc123secret';

  Widget host(AppState state) {
    return ChangeNotifierProvider<AppState>.value(
      value: state,
      child: const MaterialApp(home: RegistrationScreen()),
    );
  }

  testWidgets('pasting a valid link and tapping Use this link reveals activation UI',
      (tester) async {
    final state = AppState(serverUrl: 'ws://x/ws');
    await tester.pumpWidget(host(state));

    expect(find.text('Use this link'), findsOneWidget);

    await tester.enterText(find.byType(TextField), sampleUrl);
    await tester.tap(find.text('Use this link'));
    await tester.pump();

    expect(find.text('Activate phone'), findsOneWidget);
    expect(find.text('Display Name'), findsOneWidget);
    // Prefilled name from the link.
    expect(find.text('Rose'), findsOneWidget);
    state.dispose();
  });

  testWidgets('invalid text shows an inline error', (tester) async {
    final state = AppState(serverUrl: 'ws://x/ws');
    await tester.pumpWidget(host(state));

    await tester.enterText(find.byType(TextField), 'not a link');
    await tester.tap(find.text('Use this link'));
    await tester.pump();

    expect(find.textContaining('does not look like a valid'), findsOneWidget);
    expect(find.text('Activate phone'), findsNothing);
    state.dispose();
  });
}
