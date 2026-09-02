class Main {
  main() : Object {{
    missing + 1;
    (new Missing).call(missing_argument);
    if missing_condition then missing_left + 1 else missing_receiver.call() fi;
    let item : Missing <- missing_initializer in item.call();
    case missing_case of branch : Missing => branch + 1; esac;
    not 1;
  }};
};
