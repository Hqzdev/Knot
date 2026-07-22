import SwiftUI

struct AuthenticationView: View {
  @Bindable var store: AppStore

  @AppStorage("knot.onboarding.has-started") private var hasStarted = false
  @State private var screen = AuthenticationScreen.signIn
  @State private var email = ""
  @State private var username = ""
  @State private var password = ""
  @State private var signInIdentity = ""
  @State private var signsInWithUsername = false
  @State private var presentedSheet: AuthenticationSheet?
  @FocusState private var focusedField: AuthenticationField?

  var body: some View {
    ZStack {
      KnotStyle.primaryBackground
        .ignoresSafeArea()

      if hasStarted {
        authenticationContent
      } else {
        WelcomeView(
          onStart: startRegistration,
          onSignIn: showSignIn
        )
      }
    }
    .tint(KnotStyle.accent)
    .sheet(item: $presentedSheet) { sheet in
      switch sheet {
      case .linkDevice:
        DeviceLinkCreationView(coordinator: store.deviceLink)
      }
    }
  }

  @ViewBuilder
  private var authenticationContent: some View {
    switch screen {
    case .signIn:
      signInView
    case .registerEmail, .registerUsername, .registerPassword:
      registrationView
    }
  }

  private var registrationView: some View {
    ScrollView {
      VStack(spacing: 26) {
        authenticationNavigation(title: "Create Account", action: registrationBack)

        Spacer(minLength: 20)

        RegistrationStepHeader(
          symbol: registrationSymbol,
          title: registrationTitle,
          description: registrationDescription,
          step: registrationStep
        )

        registrationField

        if screen == .registerPassword, let error = store.authenticationError {
          AuthenticationErrorView(message: error)
        }

        Button(action: advanceRegistration) {
          HStack(spacing: 9) {
            if store.isAuthenticating {
              ProgressView()
                .controlSize(.small)
            }
            Text(screen == .registerPassword ? "Create Account" : "Continue")
              .fontWeight(.semibold)
          }
          .frame(maxWidth: .infinity)
        }
        .controlSize(.large)
        .knotProminentButton()
        .disabled(!isRegistrationStepValid || store.isAuthenticating)
        .accessibilityIdentifier("registration-continue")

        Button("I already have an account") {
          showSignIn()
        }
        .font(.subheadline.weight(.medium))
      }
      .frame(maxWidth: 420)
      .padding(.horizontal, 24)
      .padding(.bottom, 36)
      .frame(maxWidth: .infinity)
    }
    .scrollDismissesKeyboard(.interactively)
    .onAppear(perform: focusCurrentField)
    .onChange(of: screen) {
      focusCurrentField()
    }
  }

  @ViewBuilder
  private var registrationField: some View {
    switch screen {
    case .registerEmail:
      AuthenticationInput(symbol: "envelope.fill") {
        TextField("Email", text: $email)
          .textContentType(.emailAddress)
          .autocorrectionDisabled()
          .focused($focusedField, equals: .registrationEmail)
          .submitLabel(.next)
          .onSubmit(advanceRegistration)
          #if os(iOS)
            .keyboardType(.emailAddress)
            .textInputAutocapitalization(.never)
          #endif
      }
    case .registerUsername:
      AuthenticationInput(symbol: "at") {
        TextField("Username", text: $username)
          .textContentType(.username)
          .autocorrectionDisabled()
          .focused($focusedField, equals: .registrationUsername)
          .submitLabel(.next)
          .onSubmit(advanceRegistration)
          #if os(iOS)
            .textInputAutocapitalization(.never)
          #endif
      }
    case .registerPassword:
      AuthenticationInput(symbol: "key.fill") {
        SecureField("Password", text: $password)
          .textContentType(.newPassword)
          .focused($focusedField, equals: .registrationPassword)
          .submitLabel(.go)
          .onSubmit(advanceRegistration)
      }
    case .signIn:
      EmptyView()
    }
  }

  private var signInView: some View {
    ScrollView {
      VStack(spacing: 26) {
        HStack {
          Spacer()
          Button("Create Account") {
            startRegistration()
          }
          .font(.subheadline.weight(.semibold))
        }
        .frame(height: 44)

        Spacer(minLength: 12)

        KnotMark(size: 92)

        VStack(spacing: 8) {
          Text("Welcome Back")
            .font(.largeTitle.bold())
          Text(
            signsInWithUsername
              ? "Sign in with your username and password."
              : "Sign in with your email and password."
          )
          .font(.body)
          .foregroundStyle(.secondary)
          .multilineTextAlignment(.center)
        }

        VStack(spacing: 1) {
          AuthenticationInput(
            symbol: signsInWithUsername ? "at" : "envelope.fill",
            showsBackground: false
          ) {
            TextField(signsInWithUsername ? "Username" : "Email", text: $signInIdentity)
              .textContentType(signsInWithUsername ? .username : .emailAddress)
              .autocorrectionDisabled()
              .focused($focusedField, equals: .signInIdentity)
              .submitLabel(.next)
              .onSubmit { focusedField = .signInPassword }
              #if os(iOS)
                .keyboardType(signsInWithUsername ? .asciiCapable : .emailAddress)
                .textInputAutocapitalization(.never)
              #endif
          }

          Divider()
            .padding(.leading, 50)

          AuthenticationInput(symbol: "key.fill", showsBackground: false) {
            SecureField("Password", text: $password)
              .textContentType(.password)
              .focused($focusedField, equals: .signInPassword)
              .submitLabel(.go)
              .onSubmit(submitSignIn)
          }
        }
        .background(
          KnotStyle.secondaryBackground,
          in: RoundedRectangle(cornerRadius: KnotStyle.compactCornerRadius, style: .continuous)
        )

        if let error = store.authenticationError {
          AuthenticationErrorView(message: error)
        }

        Button(action: submitSignIn) {
          HStack(spacing: 9) {
            if store.isAuthenticating {
              ProgressView()
                .controlSize(.small)
            }
            Text("Sign In")
              .fontWeight(.semibold)
          }
          .frame(maxWidth: .infinity)
        }
        .controlSize(.large)
        .knotProminentButton()
        .disabled(!isSignInValid || store.isAuthenticating)
        .accessibilityIdentifier("authentication-submit")

        Button(signsInWithUsername ? "Use email instead" : "Use username for an older account") {
          signsInWithUsername.toggle()
          signInIdentity = ""
          store.clearAuthenticationError()
          focusedField = .signInIdentity
        }
        .font(.subheadline.weight(.medium))

        SecureStatusLabel()

        Button("Link Existing Device", systemImage: "qrcode") {
          presentedSheet = .linkDevice
        }
        .knotGlassButton()
      }
      .frame(maxWidth: 420)
      .padding(.horizontal, 24)
      .padding(.bottom, 36)
      .frame(maxWidth: .infinity)
    }
    .scrollDismissesKeyboard(.interactively)
  }

  private func authenticationNavigation(title: String, action: @escaping () -> Void) -> some View {
    ZStack {
      Text(title)
        .font(.headline)
      HStack {
        Button(action: action) {
          Image(systemName: "chevron.left")
            .font(.body.weight(.semibold))
            .frame(width: 40, height: 40)
        }
        .accessibilityLabel("Back")
        Spacer()
      }
    }
    .frame(height: 44)
  }

  private var registrationSymbol: String {
    switch screen {
    case .registerEmail:
      "envelope.fill"
    case .registerUsername:
      "at"
    case .registerPassword:
      "lock.fill"
    case .signIn:
      "person.fill"
    }
  }

  private var registrationTitle: String {
    switch screen {
    case .registerEmail:
      "Your Email"
    case .registerUsername:
      "Choose a Username"
    case .registerPassword:
      "Create a Password"
    case .signIn:
      "Welcome Back"
    }
  }

  private var registrationDescription: String {
    switch screen {
    case .registerEmail:
      "Enter the email address you will use to sign in."
    case .registerUsername:
      "Other people will use this username to start a secure chat with you."
    case .registerPassword:
      "Use at least 12 characters. Your password never leaves the secure connection."
    case .signIn:
      "Sign in to continue."
    }
  }

  private var registrationStep: Int {
    switch screen {
    case .registerEmail:
      1
    case .registerUsername:
      2
    case .registerPassword:
      3
    case .signIn:
      1
    }
  }

  private var isRegistrationStepValid: Bool {
    switch screen {
    case .registerEmail:
      AccountInputValidation.isValidEmail(email)
    case .registerUsername:
      AccountInputValidation.isValidUsername(username)
    case .registerPassword:
      AccountInputValidation.isValidPassword(password)
    case .signIn:
      false
    }
  }

  private var isSignInValid: Bool {
    let identityValid =
      signsInWithUsername
      ? AccountInputValidation.isValidUsername(signInIdentity)
      : AccountInputValidation.isValidEmail(signInIdentity)
    return identityValid && AccountInputValidation.isValidPassword(password)
  }

  private func startRegistration() {
    hasStarted = true
    screen = .registerEmail
    password = ""
    store.clearAuthenticationError()
  }

  private func showSignIn() {
    hasStarted = true
    screen = .signIn
    password = ""
    store.clearAuthenticationError()
    focusedField = .signInIdentity
  }

  private func registrationBack() {
    store.clearAuthenticationError()
    switch screen {
    case .registerEmail:
      showSignIn()
    case .registerUsername:
      screen = .registerEmail
    case .registerPassword:
      screen = .registerUsername
    case .signIn:
      break
    }
  }

  private func advanceRegistration() {
    guard isRegistrationStepValid, !store.isAuthenticating else {
      return
    }
    store.clearAuthenticationError()
    switch screen {
    case .registerEmail:
      screen = .registerUsername
    case .registerUsername:
      screen = .registerPassword
    case .registerPassword:
      focusedField = nil
      Task {
        await store.register(email: email, username: username, password: password)
      }
    case .signIn:
      break
    }
  }

  private func submitSignIn() {
    guard isSignInValid, !store.isAuthenticating else {
      return
    }
    focusedField = nil
    let identifier: LoginIdentifier =
      signsInWithUsername ? .username(signInIdentity) : .email(signInIdentity)
    Task {
      await store.signIn(identifier: identifier, password: password)
    }
  }

  private func focusCurrentField() {
    switch screen {
    case .signIn:
      focusedField = .signInIdentity
    case .registerEmail:
      focusedField = .registrationEmail
    case .registerUsername:
      focusedField = .registrationUsername
    case .registerPassword:
      focusedField = .registrationPassword
    }
  }
}

private struct WelcomeView: View {
  let onStart: () -> Void
  let onSignIn: () -> Void

  @State private var selectedPage = 0

  var body: some View {
    VStack(spacing: 0) {
      Spacer(minLength: 16)

      welcomePages
        .frame(maxHeight: 440)

      pageIndicator

      Spacer(minLength: 20)

      VStack(spacing: 14) {
        Button(action: onStart) {
          Text("Start Messaging")
            .fontWeight(.semibold)
            .frame(maxWidth: .infinity)
        }
        .controlSize(.large)
        .knotProminentButton()

        Button("I already have an account", action: onSignIn)
          .font(.subheadline.weight(.medium))
      }
      .frame(maxWidth: 420)
      .padding(.horizontal, 24)
      .padding(.bottom, 24)
    }
    .padding(.top, 24)
  }

  @ViewBuilder
  private var welcomePages: some View {
    #if os(iOS)
      TabView(selection: $selectedPage) {
        ForEach(WelcomePage.pages) { page in
          WelcomePageContent(page: page)
            .tag(page.id)
        }
      }
      .tabViewStyle(.page(indexDisplayMode: .never))
    #else
      WelcomePageContent(page: WelcomePage.pages[selectedPage])
    #endif
  }

  private var pageIndicator: some View {
    HStack(spacing: 8) {
      ForEach(WelcomePage.pages) { page in
        Button {
          withAnimation(.easeInOut(duration: 0.2)) {
            selectedPage = page.id
          }
        } label: {
          Capsule()
            .fill(page.id == selectedPage ? KnotStyle.accent : Color.secondary.opacity(0.2))
            .frame(width: page.id == selectedPage ? 24 : 8, height: 8)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Page \(page.id + 1) of \(WelcomePage.pages.count)")
        .accessibilityAddTraits(page.id == selectedPage ? .isSelected : [])
      }
    }
    .animation(.easeInOut(duration: 0.2), value: selectedPage)
  }
}

private struct WelcomePageContent: View {
  let page: WelcomePage

  var body: some View {
    VStack(spacing: 28) {
      KnotMark(
        size: 132,
        symbol: page.symbol,
        leadingColor: page.leadingColor,
        trailingColor: page.trailingColor
      )

      VStack(spacing: 12) {
        Text(page.title)
          .font(.system(size: 42, weight: .bold, design: .rounded))
        Text(page.description)
          .font(.title3)
          .foregroundStyle(.secondary)
          .multilineTextAlignment(.center)
      }
    }
    .padding(.horizontal, 28)
    .frame(maxWidth: .infinity, maxHeight: .infinity)
  }
}

private struct WelcomePage: Identifiable {
  let id: Int
  let symbol: String
  let title: String
  let description: String
  let leadingColor: Color
  let trailingColor: Color

  static let pages = [
    WelcomePage(
      id: 0,
      symbol: "point.3.connected.trianglepath.dotted",
      title: "Knot",
      description: "Fast, private messaging.\nSimple from the first message.",
      leadingColor: KnotStyle.accent,
      trailingColor: .cyan
    ),
    WelcomePage(
      id: 1,
      symbol: "lock.shield.fill",
      title: "Private by Default",
      description: "Messages and attachments stay\nend-to-end encrypted.",
      leadingColor: .indigo,
      trailingColor: .purple
    ),
    WelcomePage(
      id: 2,
      symbol: "arrow.triangle.2.circlepath",
      title: "Always in Sync",
      description: "Reconnect, catch up, and keep\nyour conversations available offline.",
      leadingColor: .teal,
      trailingColor: KnotStyle.accent
    ),
  ]
}

private struct KnotMark: View {
  let size: CGFloat
  let symbol: String
  let leadingColor: Color
  let trailingColor: Color

  init(
    size: CGFloat,
    symbol: String = "point.3.connected.trianglepath.dotted",
    leadingColor: Color = KnotStyle.accent,
    trailingColor: Color = .cyan
  ) {
    self.size = size
    self.symbol = symbol
    self.leadingColor = leadingColor
    self.trailingColor = trailingColor
  }

  var body: some View {
    Image(systemName: symbol)
      .font(.system(size: size * 0.42, weight: .semibold))
      .foregroundStyle(.white)
      .frame(width: size, height: size)
      .background(
        LinearGradient(
          colors: [leadingColor, trailingColor],
          startPoint: .topLeading,
          endPoint: .bottomTrailing
        ),
        in: RoundedRectangle(cornerRadius: size * 0.27, style: .continuous)
      )
      .shadow(color: KnotStyle.accent.opacity(0.25), radius: 20, y: 10)
      .accessibilityHidden(true)
  }
}

private struct RegistrationStepHeader: View {
  let symbol: String
  let title: String
  let description: String
  let step: Int

  var body: some View {
    VStack(spacing: 18) {
      Image(systemName: symbol)
        .font(.system(size: 30, weight: .semibold))
        .foregroundStyle(KnotStyle.accent)
        .frame(width: 72, height: 72)
        .background(KnotStyle.accent.opacity(0.12), in: Circle())

      VStack(spacing: 9) {
        Text(title)
          .font(.largeTitle.bold())
        Text(description)
          .font(.body)
          .foregroundStyle(.secondary)
          .multilineTextAlignment(.center)
      }

      HStack(spacing: 8) {
        ForEach(1...3, id: \.self) { index in
          Capsule()
            .fill(index <= step ? KnotStyle.accent : Color.secondary.opacity(0.18))
            .frame(width: index == step ? 28 : 8, height: 8)
        }
      }
      .animation(.easeInOut(duration: 0.2), value: step)
    }
  }
}

private struct AuthenticationInput<Content: View>: View {
  let symbol: String
  let showsBackground: Bool
  let content: Content

  init(symbol: String, showsBackground: Bool = true, @ViewBuilder content: () -> Content) {
    self.symbol = symbol
    self.showsBackground = showsBackground
    self.content = content()
  }

  @ViewBuilder
  var body: some View {
    if showsBackground {
      row
        .background(
          KnotStyle.secondaryBackground,
          in: RoundedRectangle(cornerRadius: KnotStyle.compactCornerRadius, style: .continuous)
        )
    } else {
      row
    }
  }

  private var row: some View {
    HStack(spacing: 12) {
      Image(systemName: symbol)
        .foregroundStyle(KnotStyle.accent)
        .frame(width: 24)
        .accessibilityHidden(true)
      content
        .textFieldStyle(.plain)
    }
    .padding(.horizontal, 14)
    .frame(minHeight: 54)
  }
}

private struct AuthenticationErrorView: View {
  let message: String

  var body: some View {
    Label(message, systemImage: "exclamationmark.triangle.fill")
      .font(.footnote)
      .foregroundStyle(.red)
      .frame(maxWidth: .infinity, alignment: .leading)
      .accessibilityIdentifier("authentication-error")
  }
}

private enum AuthenticationScreen: Hashable {
  case signIn
  case registerEmail
  case registerUsername
  case registerPassword
}

private enum AuthenticationField: Hashable {
  case signInIdentity
  case signInPassword
  case registrationEmail
  case registrationUsername
  case registrationPassword
}

private enum AuthenticationSheet: String, Identifiable {
  case linkDevice

  var id: Self { self }
}

#Preview("Welcome") {
  WelcomeView(onStart: {}, onSignIn: {})
}

#Preview("Registration") {
  VStack(spacing: 28) {
    RegistrationStepHeader(
      symbol: "envelope.fill",
      title: "Your Email",
      description: "Enter the email address you will use to sign in.",
      step: 1
    )
    AuthenticationInput(symbol: "envelope.fill") {
      TextField("Email", text: .constant("alice@example.com"))
    }
  }
  .padding(24)
}
