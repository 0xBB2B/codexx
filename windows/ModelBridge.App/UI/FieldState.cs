using System.Windows;

namespace ModelBridge.UI;

public static class FieldState
{
    public static readonly DependencyProperty HasErrorProperty =
        DependencyProperty.RegisterAttached("HasError", typeof(bool), typeof(FieldState), new PropertyMetadata(false));

    public static bool GetHasError(DependencyObject element) => (bool)element.GetValue(HasErrorProperty);

    public static void SetHasError(DependencyObject element, bool value) => element.SetValue(HasErrorProperty, value);
}
