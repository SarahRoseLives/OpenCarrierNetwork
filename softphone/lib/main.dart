import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import 'core/app_state.dart';
import 'features/dialer/dialer_screen.dart';
import 'features/call/call_screen.dart';
import 'features/registration/registration_screen.dart';

void main() {
  runApp(const OcnSoftphoneApp());
}

class OcnSoftphoneApp extends StatelessWidget {
  const OcnSoftphoneApp({super.key});

  @override
  Widget build(BuildContext context) {
    return ChangeNotifierProvider(
      create: (_) => AppState(serverUrl: 'ws://192.168.1.240:9100/ws')..initialize(),
      child: MaterialApp(
        title: 'OCN Phone',
        debugShowCheckedModeBanner: false,
        theme: ThemeData(
          colorSchemeSeed: Colors.blue,
          useMaterial3: true,
          brightness: Brightness.light,
        ),
        themeMode: ThemeMode.light,
        home: const AppNavigator(),
      ),
    );
  }
}

class AppNavigator extends StatelessWidget {
  const AppNavigator({super.key});

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();

    switch (appState.status) {
      case AppStatus.uninitialized:
        return const Scaffold(
          body: Center(child: CircularProgressIndicator()),
        );
      case AppStatus.needsRegistration:
        return const RegistrationScreen();
      case AppStatus.connecting:
        return const Scaffold(
          body: Center(
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                CircularProgressIndicator(),
                SizedBox(height: 16),
                Text('Connecting to server...'),
              ],
            ),
          ),
        );
      case AppStatus.connected:
        return const MainApp();
      case AppStatus.error:
        return const RegistrationScreen();
    }
  }
}

class MainApp extends StatefulWidget {
  const MainApp({super.key});

  @override
  State<MainApp> createState() => _MainAppState();
}

class _MainAppState extends State<MainApp> {
  int _currentIndex = 0;

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();

    if (appState.activeCall != null) {
      return const CallScreen();
    }

    return Scaffold(
      body: IndexedStack(
        index: _currentIndex,
        children: const [
          DialerScreen(),
          CallHistoryPlaceholder(),
          ContactsPlaceholder(),
          SettingsScreen(),
        ],
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _currentIndex,
        onDestinationSelected: (index) {
          setState(() => _currentIndex = index);
        },
        destinations: const [
          NavigationDestination(icon: Icon(Icons.dialpad), label: 'Dialer'),
          NavigationDestination(icon: Icon(Icons.history), label: 'History'),
          NavigationDestination(icon: Icon(Icons.contacts), label: 'Contacts'),
          NavigationDestination(icon: Icon(Icons.settings), label: 'Settings'),
        ],
      ),
    );
  }
}

class CallHistoryPlaceholder extends StatelessWidget {
  const CallHistoryPlaceholder({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Call History')),
      body: const Center(
        child: Text('Call history will appear here'),
      ),
    );
  }
}

class ContactsPlaceholder extends StatelessWidget {
  const ContactsPlaceholder({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Contacts')),
      body: const Center(
        child: Text('Contacts will appear here'),
      ),
    );
  }
}

class SettingsScreen extends StatelessWidget {
  const SettingsScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();

    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        children: [
          ListTile(
            leading: const Icon(Icons.phone),
            title: const Text('Phone Number'),
            subtitle: Text(appState.phoneNumber?.formatted ?? 'Not registered'),
          ),
          ListTile(
            leading: const Icon(Icons.person),
            title: const Text('Display Name'),
            subtitle: Text(appState.displayName),
          ),
          ListTile(
            leading: const Icon(Icons.dns),
            title: const Text('Server'),
            subtitle: Text(appState.serverUrl),
          ),
          const Divider(),
          ListTile(
            leading: const Icon(Icons.key),
            title: const Text('kSIM Identity'),
            subtitle: Text(
              appState.keypair != null
                  ? 'Active'
                  : 'Not loaded',
            ),
          ),
          const Divider(),
          ListTile(
            leading: const Icon(Icons.logout, color: Colors.red),
            title: const Text('Logout', style: TextStyle(color: Colors.red)),
            subtitle: const Text('Remove kSIM and disconnect'),
            onTap: () {
              showDialog(
                context: context,
                builder: (ctx) => AlertDialog(
                  title: const Text('Logout'),
                  content: const Text('This will remove your kSIM identity from this device. You will need to re-register to use this number again.'),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.pop(ctx),
                      child: const Text('Cancel'),
                    ),
                    TextButton(
                      onPressed: () {
                        Navigator.pop(ctx);
                        appState.logout();
                      },
                      child: const Text('Logout', style: TextStyle(color: Colors.red)),
                    ),
                  ],
                ),
              );
            },
          ),
        ],
      ),
    );
  }
}
