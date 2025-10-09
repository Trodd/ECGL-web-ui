import { Component } from "react";

class ErrorBoundary extends Component {
  constructor(props) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error) {
    // Update state so next render shows fallback
    return { hasError: true, error };
  }

  componentDidCatch(error, info) {
    console.error("❌ React crashed:", error, info);
  }

  handleReload = () => {
    this.setState({ hasError: false, error: null });
    window.location.reload();
  };

  render() {
    if (this.state.hasError) {
      return (
        <div className="alert alert-danger mt-3">
          <h4>⚠️ Something went wrong.</h4>
          <p>{this.state.error?.message || "An unexpected error occurred."}</p>
          <button className="btn btn-sm btn-primary mt-2" onClick={this.handleReload}>
            🔄 Reload Page
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}

export default ErrorBoundary;
