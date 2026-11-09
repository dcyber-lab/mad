import Foundation

/// The words mad shows, in Chinese or English after the system's language. They are written in Chinese in the
/// code (the catalog's source language) and translated in each package's Localizable.xcstrings; tool names,
/// claude and codex, commands, what an agent wrote and the accessibility labels the UI tests look for are not
/// in it. Each package looks its words up in its own bundle (`L` and `Lf`, internal to each).
public enum MadLocale {
    /// A language to use instead of the system's ("zh-Hans", "en"); nil follows the system. Tests set it so
    /// that what they assert does not depend on the machine's language.
    public static var language: String?

    /// The word for `key` (its Chinese) in `bundle`, in the language in use; the key itself when it has none.
    public static func string(_ key: String, bundle: Bundle) -> String {
        localized(bundle).localizedString(forKey: key, value: key, table: nil)
    }

    /// The format for `key` filled with `args` (`%lld` for a number, `%@` for a text).
    public static func format(_ key: String, bundle: Bundle, _ args: [CVarArg]) -> String {
        String(format: string(key, bundle: bundle), locale: Locale.current, arguments: args)
    }

    /// Whether the words come out in English: dates and the like are put together to match.
    public static func isEnglish(bundle: Bundle) -> Bool {
        if let language { return language.hasPrefix("en") }
        return bundle.preferredLocalizations.first?.hasPrefix("en") ?? false
    }

    /// The bundle of the chosen language's .lproj, when one is chosen and the bundle has it.
    private static func localized(_ bundle: Bundle) -> Bundle {
        guard let language, let path = bundle.path(forResource: language, ofType: "lproj"), let b = Bundle(path: path) else { return bundle }
        return b
    }
}

/// A word of MadUI's, in the language in use.
func L(_ key: String) -> String { MadLocale.string(key, bundle: .module) }

/// A format of MadUI's, filled in.
func Lf(_ key: String, _ args: CVarArg...) -> String { MadLocale.format(key, bundle: .module, args) }

/// Whether MadUI's words are English now.
var madEnglish: Bool { MadLocale.isEnglish(bundle: .module) }
